package ipc

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Handler executes one request. Implementations must be safe for concurrent
// read calls; mutation serialization belongs to the handler.
type Handler func(ctx context.Context, request Request) Response

// Server accepts local supervisor connections.
type Server struct {
	listener   net.Listener
	handler    Handler
	timeout    time.Duration
	timeoutFor func(Request) time.Duration

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closing bool
	wg      sync.WaitGroup
}

// SetTimeoutFor overrides the per-request operation deadline. Read-only
// operations should stay short while mutations may legitimately run for
// minutes; the connection read deadline remains independent.
func (s *Server) SetTimeoutFor(fn func(Request) time.Duration) { s.timeoutFor = fn }

// NewServer builds a server around listener and handler.
func NewServer(listener net.Listener, handler Handler) *Server {
	return &Server{
		listener: listener,
		handler:  handler,
		timeout:  30 * time.Second,
		conns:    map[net.Conn]struct{}{},
	}
}

// Serve accepts connections until the listener is closed or ctx ends.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.listener.Close()
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if err := verifyPeer(conn); err != nil {
			_ = conn.Close()
			continue
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		// Add is serialized with Close's closing flag. This prevents an Add
		// racing with Wait when Close wins the accept loop.
		s.wg.Add(1)
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		s.wg.Done()
		_ = conn.Close()
	}()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(s.timeout + 5*time.Second))
		var request Request
		if err := ReadMessage(conn, &request); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		response := s.handle(ctx, request)
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := WriteMessage(conn, response); err != nil {
			return
		}
	}
}

func (s *Server) handle(parent context.Context, request Request) Response {
	if request.RequestID == "" {
		return errorResponse(request, CodeBadRequest, "missing request_id")
	}
	if request.Version != Version {
		return errorResponse(request, CodeVersionMismatch, "protocol version mismatch")
	}
	timeout := s.timeout
	if s.timeoutFor != nil {
		if custom := s.timeoutFor(request); custom > 0 {
			timeout = custom
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return s.handler(ctx, request)
}

// Close stops accepting and closes active connections.
func (s *Server) Close() {
	s.mu.Lock()
	s.closing = true
	_ = s.listener.Close()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func errorResponse(request Request, code, message string) Response {
	return Response{
		Version:   Version,
		RequestID: request.RequestID,
		Success:   false,
		Error:     &RPCError{Code: code, Message: message},
	}
}
