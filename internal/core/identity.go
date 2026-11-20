package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrProcessNotFound reports that no live process exists for a PID.
var ErrProcessNotFound = errors.New("process not found")

// ErrProcessIdentityUnavailable reports that the process exists but the
// platform refused or failed to expose enough information to identify it.
var ErrProcessIdentityUnavailable = errors.New("process identity unavailable")

// ProcessIdentity is the stable, OS-provided identity of one process.
type ProcessIdentity struct {
	PID        int
	StartedAt  time.Time
	Executable string
}

// IdentityVerdict classifies a persisted core state against the live process.
type IdentityVerdict string

const (
	IdentityVerified   IdentityVerdict = "verified"
	IdentityMismatch   IdentityVerdict = "mismatch"
	IdentityUnverified IdentityVerdict = "unverified"
	ProcessGone        IdentityVerdict = "gone"
)

// ReadProcessIdentity returns the OS identity for pid.
func ReadProcessIdentity(pid int) (ProcessIdentity, error) {
	if pid <= 0 {
		return ProcessIdentity{}, ErrProcessNotFound
	}
	return readProcessIdentity(pid)
}

// ProcessAlive reports whether a process with this PID exists. Permission
// failures count as "unknown" and are returned as false; callers that need to
// distinguish should use ReadProcessIdentity.
func ProcessAlive(pid int) bool {
	_, err := ReadProcessIdentity(pid)
	return err == nil
}

// HashFile returns the SHA-256 of a file, or an empty string when unreadable.
func HashFile(path string) string {
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, 512<<20)); err != nil {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// HashBytes returns the SHA-256 of a byte slice.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// VerifyRuntimeState compares persisted state with the live OS process.
// full additionally verifies binary and config hashes; the fast path is safe
// for frequent status polling. Legacy states without the new identity fields
// are deliberately classified as unverified, never trusted.
func VerifyRuntimeState(state RuntimeState, full bool) (ProcessIdentity, IdentityVerdict, error) {
	identity, err := ReadProcessIdentity(state.PID)
	if errors.Is(err, ErrProcessNotFound) {
		return ProcessIdentity{}, ProcessGone, nil
	}
	if err != nil {
		return ProcessIdentity{}, IdentityUnverified, err
	}
	if state.ProcessStartedAt.IsZero() || state.BinaryPath == "" {
		return identity, IdentityUnverified, nil
	}
	delta := identity.StartedAt.Sub(state.ProcessStartedAt)
	if delta < 0 {
		delta = -delta
	}
	if delta > 2*time.Second {
		return identity, IdentityMismatch, nil
	}
	if !samePath(identity.Executable, state.BinaryPath) {
		return identity, IdentityMismatch, nil
	}
	if !full {
		return identity, IdentityVerified, nil
	}
	if state.BinarySHA256 == "" || state.ConfigPath == "" || state.ConfigSHA256 == "" {
		return identity, IdentityUnverified, nil
	}
	if actual := HashFile(state.BinaryPath); actual == "" || !strings.EqualFold(actual, state.BinarySHA256) {
		return identity, IdentityMismatch, nil
	}
	if actual := HashFile(state.ConfigPath); actual == "" || !strings.EqualFold(actual, state.ConfigSHA256) {
		return identity, IdentityMismatch, nil
	}
	return identity, IdentityVerified, nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	cleanA := filepath.Clean(a)
	cleanB := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(cleanA, cleanB)
	}
	return cleanA == cleanB
}

// WaitProcessExit polls until pid disappears or timeout elapses.
func WaitProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := ReadProcessIdentity(pid); errors.Is(err, ErrProcessNotFound) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ErrCoreIdentityMismatch is the sentinel used by adapters for an
// unverifiable or mismatched core process.
var ErrCoreIdentityMismatch = errors.New("CORE_IDENTITY_MISMATCH")
