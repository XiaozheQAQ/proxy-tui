package supervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// StatusPayload is returned by supervisor.status and core.status.
type StatusPayload struct {
	PID       int                            `json:"pid"`
	StartedAt time.Time                      `json:"started_at"`
	Endpoint  string                         `json:"endpoint"`
	Version   string                         `json:"version"`
	Protocol  int                            `json:"protocol"`
	Cores     map[model.CoreKind]core.Status `json:"cores"`
}

// CoreAction is returned by core.start/stop/restart.
type CoreAction struct {
	Core    model.CoreKind `json:"core"`
	State   string         `json:"state"`
	Already bool           `json:"already,omitempty"`
}

// ProxiesPayload is returned by proxies.list.
type ProxiesPayload struct {
	Proxies []core.Proxy `json:"proxies"`
}

// NetworkPayload is returned by network.status.
type NetworkPayload struct {
	Report network.Report `json:"report"`
}

// LogsPayload is returned by logs.tail.
type LogsPayload struct {
	Entries []logging.Entry `json:"entries"`
	Path    string          `json:"path"`
}

type handler struct {
	backend  Backend
	endpoint string
	version  string
	started  time.Time
	logPath  string
	cancel   context.CancelFunc

	mutation    sync.Mutex
	stopped     atomic.Bool
	stopping    atomic.Bool
	persistStop func() error

	cacheMu sync.Mutex
	cache   map[string]cacheEntry
}

type cacheEntry struct {
	operation   string
	fingerprint string
	response    ipc.Response
	storedAt    time.Time
	running     bool
}

// OperationStatusPayload reports the lifecycle of one mutation.
type OperationStatusPayload struct {
	OperationID string          `json:"operation_id"`
	Status      string          `json:"status"`
	Error       *ipc.RPCError   `json:"error,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	StoredAt    time.Time       `json:"stored_at,omitempty"`
}

const (
	readOperationTimeout = 30 * time.Second
	mutationTimeout      = 15 * time.Minute
	operationCacheMax    = 256
)

// operationCacheTTL is a variable so cache expiry can be tested without
// waiting five minutes. It applies to terminal results only; RUNNING entries
// are never expired and re-executed.
var operationCacheTTL = 5 * time.Minute

// NewHandler builds the RPC handler.
func NewHandler(backend Backend, endpoint, version, logPath string, cancel context.CancelFunc, persistStop func() error) *handler {
	h := &handler{
		backend:     backend,
		endpoint:    endpoint,
		version:     version,
		started:     time.Now(),
		logPath:     logPath,
		cancel:      cancel,
		persistStop: persistStop,
		cache:       map[string]cacheEntry{},
	}
	return h
}

// timeoutFor separates short read deadlines from long business mutations.
func (h *handler) timeoutFor(request ipc.Request) time.Duration {
	if h.isMutationRequest(request) {
		return mutationTimeout
	}
	return readOperationTimeout
}

func (h *handler) handle(ctx context.Context, request ipc.Request) ipc.Response {
	if request.Operation == ipc.OpOperationGet {
		return h.operationStatus(request)
	}
	if request.OperationID != "" && h.isMutationRequest(request) {
		fingerprint := requestFingerprint(request)
		cached, claimed, err := h.claimOperation(request, fingerprint)
		if err != nil {
			return failResponse(request, ipc.CodeBadRequest, err.Error())
		}
		if !claimed {
			return cached
		}
		response := h.safeDispatch(ctx, request)
		if !response.Success && ctx.Err() == context.DeadlineExceeded {
			response = failResponse(request, ipc.CodeOperationTimeout, "operation exceeded its business deadline")
		}
		h.finishOperation(request.OperationID, fingerprint, response)
		return response
	}
	return h.safeDispatch(ctx, request)
}

// safeDispatch converts a backend panic into a normal failed response instead
// of crashing the supervisor and leaving a RUNNING operation orphaned.
func (h *handler) safeDispatch(ctx context.Context, request ipc.Request) (response ipc.Response) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = failResponse(request, ipc.CodeInternal, fmt.Sprintf("handler panic: %v", recovered))
		}
	}()
	return h.dispatch(ctx, request)
}

func (h *handler) operationStatus(request ipc.Request) ipc.Response {
	var payload struct {
		OperationID string `json:"operation_id"`
	}
	if err := decode(request, &payload); err != nil || payload.OperationID == "" {
		return failResponse(request, ipc.CodeBadRequest, "operation_id is required")
	}
	h.cacheMu.Lock()
	entry, ok := h.cache[payload.OperationID]
	expired := ok && !entry.running && time.Since(entry.storedAt) > operationCacheTTL
	h.cacheMu.Unlock()
	status := "UNKNOWN"
	result := OperationStatusPayload{OperationID: payload.OperationID}
	switch {
	case !ok:
		status = "UNKNOWN"
	case entry.running:
		// A running operation is never reported as expired: that would let a
		// retry execute it a second time while the first is still in flight.
		status = "RUNNING"
	case expired:
		status = "EXPIRED"
	case entry.response.Success:
		status = "SUCCEEDED"
		result.Payload = entry.response.Payload
	case entry.response.Error != nil:
		status = "FAILED"
		result.Error = entry.response.Error
	}
	result.Status = status
	if ok {
		result.StoredAt = entry.storedAt
	}
	return okResponse(request, result)
}

func (h *handler) dispatch(ctx context.Context, request ipc.Request) ipc.Response {
	if h.stopping.Load() && h.isMutationRequest(request) {
		return failResponse(request, ipc.CodeNetworkBlocked, "supervisor is stopping; mutations are disabled")
	}
	switch request.Operation {
	case ipc.OpPing:
		return okResponse(request, map[string]string{"status": "ok"})
	case ipc.OpSupervisorStatus, ipc.OpCoreStatus:
		return h.status(ctx, request)
	case ipc.OpSupervisorStop:
		return h.stop(ctx, request)
	case ipc.OpCoreStart, ipc.OpCoreStop, ipc.OpCoreRestart:
		return h.coreAction(ctx, request)
	case ipc.OpProxiesList:
		return h.proxies(ctx, request)
	case ipc.OpProxySelect:
		return h.selectProxy(ctx, request)
	case ipc.OpNetworkStatus:
		report, err := h.backend.NetworkReport(ctx)
		if err != nil {
			return failResponse(request, ipc.CodeInternal, err.Error())
		}
		return okResponse(request, NetworkPayload{Report: report})
	case ipc.OpLogsTail, ipc.OpSupervisorLogTail:
		return h.logs(ctx, request)
	case ipc.OpAppCall:
		return h.appCall(ctx, request)
	default:
		return failResponse(request, ipc.CodeUnsupported, "unknown operation "+request.Operation)
	}
}

func (h *handler) status(ctx context.Context, request ipc.Request) ipc.Response {
	statuses := h.backend.Statuses(ctx)
	return okResponse(request, StatusPayload{
		PID:       os.Getpid(),
		StartedAt: h.started,
		Endpoint:  h.endpoint,
		Version:   h.version,
		Protocol:  ipc.Version,
		Cores:     statuses,
	})
}

// Stopped reports whether this supervisor received a graceful stop request.
func (h *handler) Stopped() bool { return h.stopped.Load() }

func (h *handler) stop(ctx context.Context, request ipc.Request) ipc.Response {
	h.mutation.Lock()
	defer h.mutation.Unlock()

	if h.stopping.Load() {
		return okResponse(request, map[string]bool{"stopping": true})
	}
	for kind, status := range h.backend.Statuses(ctx) {
		if status.Running {
			return failResponse(request, ipc.CodeNetworkBlocked,
				fmt.Sprintf("%s is still running; stop the core before stopping the supervisor", kind.DisplayName()))
		}
	}
	tunStatus, err := h.backend.TUNStatus(ctx)
	if err != nil {
		return failResponse(request, ipc.CodeInternal, err.Error())
	}
	if tunStatus.Enabled {
		return failResponse(request, ipc.CodeNetworkBlocked, "TUN is still enabled; disable it and verify release first")
	}
	report, err := h.backend.NetworkReport(ctx)
	if err != nil {
		return failResponse(request, ipc.CodeInternal, err.Error())
	}
	if report.BackupActive {
		return failResponse(request, ipc.CodeNetworkBlocked, "proxy-tui still owns the desktop proxy; restore it first")
	}
	if h.persistStop != nil {
		if err := h.persistStop(); err != nil {
			return failResponse(request, ipc.CodeInternal, "persist stop intent: "+err.Error())
		}
	}
	// Reject new mutations before the final shutdown window. A mutation
	// that already holds the mutex above completes before we get here.
	h.stopping.Store(true)
	h.stopped.Store(true)
	go func() {
		time.Sleep(80 * time.Millisecond)
		h.cancel()
	}()
	return okResponse(request, map[string]bool{"stopping": true})
}

func (h *handler) coreAction(ctx context.Context, request ipc.Request) ipc.Response {
	var payload struct {
		Core string `json:"core"`
	}
	if err := decode(request, &payload); err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	kind, err := model.ParseCoreKind(payload.Core)
	if err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	if h.stopping.Load() {
		return failResponse(request, ipc.CodeNetworkBlocked, "supervisor is stopping; mutations are disabled")
	}

	status := h.backend.Statuses(ctx)[kind]
	switch request.Operation {
	case ipc.OpCoreStart:
		if status.Running {
			return failResponse(request, ipc.CodeCoreAlreadyRunning, kind.DisplayName()+" is already running")
		}
		if err := h.backend.StartCore(ctx, kind); err != nil {
			return failResponse(request, ipc.CodeInternal, err.Error())
		}
		if !h.backend.Statuses(ctx)[kind].Running {
			return failResponse(request, ipc.CodeInternal, "core did not report RUNNING after start")
		}
		return okResponse(request, CoreAction{Core: kind, State: "running"})
	case ipc.OpCoreStop:
		if !status.Running {
			return okResponse(request, CoreAction{Core: kind, State: "stopped", Already: true})
		}
		if err := h.backend.StopCore(ctx, kind); err != nil {
			return failResponse(request, ipc.CodeInternal, err.Error())
		}
		return okResponse(request, CoreAction{Core: kind, State: "stopped"})
	case ipc.OpCoreRestart:
		if err := h.backend.RestartCore(ctx, kind); err != nil {
			return failResponse(request, ipc.CodeInternal, err.Error())
		}
		return okResponse(request, CoreAction{Core: kind, State: "running"})
	}
	return failResponse(request, ipc.CodeUnsupported, "unsupported core action")
}

func (h *handler) proxies(ctx context.Context, request ipc.Request) ipc.Response {
	var payload struct {
		Core string `json:"core"`
	}
	if err := decode(request, &payload); err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	kind, err := model.ParseCoreKind(payload.Core)
	if err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	proxies, err := h.backend.Proxies(ctx, kind)
	if err != nil {
		return failResponse(request, ipc.CodeInternal, err.Error())
	}
	return okResponse(request, ProxiesPayload{Proxies: proxies})
}

func (h *handler) selectProxy(ctx context.Context, request ipc.Request) ipc.Response {
	var payload struct {
		Core  string `json:"core"`
		Group string `json:"group"`
		Name  string `json:"name"`
	}
	if err := decode(request, &payload); err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	kind, err := model.ParseCoreKind(payload.Core)
	if err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	if strings.TrimSpace(payload.Group) == "" || strings.TrimSpace(payload.Name) == "" {
		return failResponse(request, ipc.CodeBadRequest, "group and name are required")
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	if h.stopping.Load() {
		return failResponse(request, ipc.CodeNetworkBlocked, "supervisor is stopping; mutations are disabled")
	}
	if !h.backend.Statuses(ctx)[kind].Running {
		return failResponse(request, ipc.CodeCoreNotRunning, kind.DisplayName()+" is not running")
	}
	if err := h.backend.SelectProxy(ctx, kind, payload.Group, payload.Name); err != nil {
		return failResponse(request, ipc.CodeInternal, err.Error())
	}
	return okResponse(request, CoreAction{Core: kind, State: "selected"})
}

func (h *handler) appCall(ctx context.Context, request ipc.Request) ipc.Response {
	var payload app.AppCallPayload
	if err := decode(request, &payload); err != nil {
		return failResponse(request, ipc.CodeBadRequest, err.Error())
	}
	target := h.backend.Callable()
	if target == nil {
		return failResponse(request, ipc.CodeUnsupported, "frontend calls are not available")
	}
	if isFacadeMutation(payload.Method) {
		h.mutation.Lock()
		defer h.mutation.Unlock()
		if h.stopping.Load() {
			return failResponse(request, ipc.CodeNetworkBlocked, "supervisor is stopping; mutations are disabled")
		}
	}
	result, err := invokeFacade(ctx, target, payload.Method, payload.Args)
	if err != nil {
		return failResponse(request, ipc.CodeInternal, err.Error())
	}
	return okResponse(request, result)
}

func isFacadeMutation(method string) bool {
	switch method {
	case "CoreInstall", "CoreStart", "CoreStop", "CoreRestart", "CoreSelect",
		"SubsSetActive", "SubsDelete", "SubsUpdate", "SubsUpdateAll", "SubsSetDefaultInterval",
		"UpdateSettings", "PatchSettings", "LogsClear", "Import", "ApplyTrafficMode", "RecoverTUN",
		"RecoverNetwork", "TUNDisable":
		return true
	}
	return false
}

func (h *handler) logs(ctx context.Context, request ipc.Request) ipc.Response {
	var payload struct {
		Limit int `json:"limit"`
	}
	_ = decode(request, &payload)
	limit := payload.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}
	if request.Operation == ipc.OpSupervisorLogTail {
		return okResponse(request, LogsPayload{Path: h.logPath, Entries: []logging.Entry{}})
	}
	return okResponse(request, LogsPayload{Entries: h.backend.LogsTail(limit), Path: h.logPath})
}

// claimOperation atomically either returns an existing result/progress
// response or creates the RUNNING entry for a new operation. Check-and-set in
// one cacheMu critical section is what makes duplicate IDs safe under
// concurrency.
func (h *handler) claimOperation(request ipc.Request, fingerprint string) (ipc.Response, bool, error) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()

	if entry, ok := h.cache[request.OperationID]; ok {
		if entry.fingerprint != fingerprint {
			return ipc.Response{}, false, fmt.Errorf("operation_id %q was already used for a different request", request.OperationID)
		}
		if entry.running {
			return failResponse(request, ipc.CodeOperationInProgress, "operation is still running; query operation.get"), false, nil
		}
		if time.Since(entry.storedAt) <= operationCacheTTL {
			cached := entry.response
			cached.RequestID = request.RequestID
			return cached, false, nil
		}
		delete(h.cache, request.OperationID)
	}

	h.evictTerminalLocked()
	h.cache[request.OperationID] = cacheEntry{
		operation:   request.Operation,
		fingerprint: fingerprint,
		storedAt:    time.Now(),
		running:     true,
	}
	return ipc.Response{}, true, nil
}

func (h *handler) finishOperation(operationID, fingerprint string, response ipc.Response) {
	if operationID == "" {
		return
	}
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	entry, ok := h.cache[operationID]
	if !ok || entry.fingerprint != fingerprint {
		return
	}
	entry.response = response
	entry.running = false
	// The terminal TTL starts when the operation completed, not when it
	// started; long mutations must not look expired immediately.
	entry.storedAt = time.Now()
	h.cache[operationID] = entry
}

// evictTerminalLocked removes expired terminal entries, then the oldest
// terminal entries. RUNNING operations are never evicted.
func (h *handler) evictTerminalLocked() {
	if len(h.cache) < operationCacheMax {
		return
	}
	for id, entry := range h.cache {
		if !entry.running && time.Since(entry.storedAt) > operationCacheTTL {
			delete(h.cache, id)
		}
	}
	for len(h.cache) >= operationCacheMax {
		oldestID := ""
		var oldest time.Time
		for id, entry := range h.cache {
			if entry.running {
				continue
			}
			if oldestID == "" || entry.storedAt.Before(oldest) {
				oldestID, oldest = id, entry.storedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(h.cache, oldestID)
	}
}

func requestFingerprint(request ipc.Request) string {
	sum := sha256.Sum256(request.Payload)
	return request.Operation + ":" + hex.EncodeToString(sum[:])
}

func (h *handler) isMutationRequest(request ipc.Request) bool {
	switch request.Operation {
	case ipc.OpCoreStart, ipc.OpCoreStop, ipc.OpCoreRestart, ipc.OpProxySelect:
		return true
	case ipc.OpAppCall:
		var payload app.AppCallPayload
		if err := decode(request, &payload); err != nil {
			return false
		}
		return isFacadeMutation(payload.Method)
	}
	return false
}

func okResponse(request ipc.Request, payload any) ipc.Response {
	data, err := json.Marshal(payload)
	if err != nil {
		return failResponse(request, ipc.CodeInternal, err.Error())
	}
	return ipc.Response{Version: ipc.Version, RequestID: request.RequestID, Success: true, Payload: data}
}

func failResponse(request ipc.Request, code, message string) ipc.Response {
	return ipc.Response{
		Version:   ipc.Version,
		RequestID: request.RequestID,
		Success:   false,
		Error:     &ipc.RPCError{Code: code, Message: message},
	}
}

func decode(request ipc.Request, out any) error {
	if len(request.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(request.Payload, out)
}
