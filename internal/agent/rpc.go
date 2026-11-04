package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// rpcConn is newline-delimited JSON-RPC over a process's stdin/stdout, as
// used by `codex app-server`: calls to the CLI, and the CLI's notifications
// and requests (approvals) to us.
//
// It's hand-rolled because Codex leaves out the "jsonrpc":"2.0" field, which
// JSON-RPC 2.0 libraries require, and because it's small. An ACP agent should
// use github.com/coder/acp-go-sdk instead, which has its own connection.
type rpcConn struct {
	send   func(v any) error
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan rpcMessage
	closed  chan struct{}

	onNotify  func(method string, params json.RawMessage)
	onRequest func(method string, params json.RawMessage) (any, error)
}

// newRPCConn reads messages from r and writes them with send. The handlers
// are set before the read loop starts.
func newRPCConn(r io.Reader, send func(v any) error,
	onNotify func(method string, params json.RawMessage),
	onRequest func(method string, params json.RawMessage) (any, error),
) *rpcConn {
	c := &rpcConn{send: send, onNotify: onNotify, onRequest: onRequest, pending: map[int64]chan rpcMessage{}, closed: make(chan struct{})}
	go c.readLoop(r)
	return c
}

// Call sends a request and waits for its result, the process exiting, or ctx.
func (c *rpcConn) Call[T any](ctx context.Context, method string, params any) (T, error) {
	var zero T
	id := c.nextID.Add(1)
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return zero, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return zero, fmt.Errorf("%s: %w", method, m.Error)
		}
		if m.Result == nil {
			return zero, nil
		}
		return decode[T](m.Result)
	case <-c.closed:
		return zero, fmt.Errorf("%s: %w", method, ErrStopped)
	case <-ctx.Done():
		return zero, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (c *rpcConn) Notify(method string, params any) error {
	msg := map[string]any{"method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.send(msg)
}

func (c *rpcConn) readLoop(r io.Reader) {
	defer close(c.closed)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		m, err := decode[rpcMessage](sc.Bytes())
		if err != nil {
			continue
		}
		switch {
		case m.Method != "" && m.ID != nil: // request from the server
			go c.answer(m)
		case m.Method != "": // notification
			c.onNotify(m.Method, m.Params)
		case m.ID != nil: // response
			id, err := decode[int64](*m.ID)
			if err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

func (c *rpcConn) answer(m rpcMessage) {
	result, err := c.onRequest(m.Method, m.Params)
	reply := map[string]any{"id": m.ID}
	if err != nil {
		reply["error"] = rpcError{Code: jsonrpc.CodeMethodNotFound, Message: err.Error()}
	} else {
		reply["result"] = result
	}
	_ = c.send(reply)
}

type rpcMessage struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }
