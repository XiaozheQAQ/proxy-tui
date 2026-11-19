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
	listener net.Listener
	handler  Handler
	timeout  time.Duration

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

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
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
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
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	return s.handler(ctx, request)
}

// Close stops accepting and closes active connections.
func (s *Server) Close() {
	_ = s.listener.Close()
	s.mu.Lock()
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
