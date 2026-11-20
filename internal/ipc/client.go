package ipc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Client is one supervisor connection. Calls are serialized per connection.
type Client struct {
	conn      net.Conn
	timeout   time.Duration
	mu        sync.Mutex
	sequence  atomic.Uint64
	requestID func() string
}

// NewClient wraps an established connection.
func NewClient(conn net.Conn, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{conn: conn, timeout: timeout}
}

// Dial connects to the supervisor and returns a client.
func Dial(ctx context.Context, endpoint string, timeout time.Duration) (*Client, error) {
	conn, err := DialConn(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return NewClient(conn, timeout), nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call performs one request/response round trip.
func (c *Client) Call(ctx context.Context, operation string, payload any, out any) error {
	return c.CallWithOperationID(ctx, operation, payload, out, "")
}

// CallWithOperationID attaches a stable operation ID so a retry after a lost
// response cannot execute the mutation twice.
func (c *Client) CallWithOperationID(ctx context.Context, operation string, payload any, out any, operationID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := fmt.Sprintf("req-%d", c.sequence.Add(1))
	request := Request{Version: Version, RequestID: id, OperationID: operationID, Operation: operation}
	if payload != nil {
		data, err := marshal(payload)
		if err != nil {
			return err
		}
		request.Payload = data
	}
	// Honour the caller's deadline when it has one. A longer business
	// deadline (installation, TUN activation) must not be truncated by the
	// transport default; callers that need a short read still set a short
	// context deadline.
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok {
		deadline = ctxDeadline
	}
	_ = c.conn.SetDeadline(deadline)
	if err := WriteMessage(c.conn, request); err != nil {
		return fmt.Errorf("send %s: %w", operation, err)
	}
	var response Response
	if err := ReadMessage(c.conn, &response); err != nil {
		return fmt.Errorf("read %s: %w", operation, err)
	}
	if response.Version != Version {
		return &RPCError{Code: CodeVersionMismatch, Message: fmt.Sprintf("server protocol %d, client %d", response.Version, Version)}
	}
	if response.RequestID != id {
		return &RPCError{Code: CodeInternal, Message: "response request_id does not match"}
	}
	if !response.Success {
		if response.Error == nil {
			return &RPCError{Code: CodeInternal, Message: "request failed without an error"}
		}
		return response.Error
	}
	if out == nil {
		return nil
	}
	return unmarshal(response.Payload, out)
}
