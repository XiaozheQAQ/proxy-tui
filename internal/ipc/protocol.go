// Package ipc implements the local JSON-RPC protocol used between clients and
// the per-user supervisor.
package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Version is the protocol version. Clients and the server must agree.
const Version = 1

// MaxMessageSize bounds one framed request or response (1 MiB).
const MaxMessageSize = 1 << 20

// Operation names.
const (
	OpPing              = "ping"
	OpSupervisorStatus  = "supervisor.status"
	OpSupervisorStop    = "supervisor.stop"
	OpCoreStatus        = "core.status"
	OpCoreStart         = "core.start"
	OpCoreStop          = "core.stop"
	OpCoreRestart       = "core.restart"
	OpProxiesList       = "proxies.list"
	OpProxySelect       = "proxy.select"
	OpNetworkStatus     = "network.status"
	OpLogsTail          = "logs.tail"
	OpSupervisorLogTail = "supervisor.logs"
	OpAppCall           = "app.call"
)

// Error codes shared by the supervisor and clients.
const (
	CodeSupervisorUnavailable = "SUPERVISOR_UNAVAILABLE"
	CodeVersionMismatch       = "PROTOCOL_VERSION_MISMATCH"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeCoreAlreadyRunning    = "CORE_ALREADY_RUNNING"
	CodeCoreNotRunning        = "CORE_NOT_RUNNING"
	CodeCoreIdentityMismatch  = "CORE_IDENTITY_MISMATCH"
	CodeProfileNotFound       = "PROFILE_NOT_FOUND"
	CodeNetworkBlocked        = "NETWORK_BLOCKED"
	CodeOperationInProgress   = "OPERATION_IN_PROGRESS"
	CodeOperationTimeout      = "OPERATION_TIMEOUT"
	CodeUnsupported           = "UNSUPPORTED_OPERATION"
	CodeInternal              = "INTERNAL_ERROR"
	CodeBadRequest            = "BAD_REQUEST"
)

// Request is one RPC request.
type Request struct {
	Version   int             `json:"version"`
	RequestID string          `json:"request_id"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Response is one RPC response.
type Response struct {
	Version   int             `json:"version"`
	RequestID string          `json:"request_id"`
	Success   bool            `json:"success"`
	Error     *RPCError       `json:"error,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// RPCError is a standardized error.
type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// WriteMessage writes one length-prefixed JSON value.
func WriteMessage(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes", len(data))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadMessage reads one length-prefixed JSON value into out.
func ReadMessage(r io.Reader, out any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxMessageSize {
		return fmt.Errorf("invalid message size: %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
