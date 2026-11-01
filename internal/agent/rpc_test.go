package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
)

// TestRPCCallDecodesResult answers each request the way codex app-server
// does: no "jsonrpc" field, and a result that may be left out.
func TestRPCCallDecodesResult(t *testing.T) {
	toClient, server := io.Pipe()
	requests, toServer := io.Pipe()
	send := func(v any) error {
		b, _ := json.Marshal(v)
		_, err := toServer.Write(append(b, '\n'))
		return err
	}
	go func() {
		sc := bufio.NewScanner(requests)
		for sc.Scan() {
			req, _ := decode[struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}](sc.Bytes())
			reply := map[string]any{"id": req.ID}
			if req.Method == "thread/start" {
				reply["result"] = map[string]any{"thread": map[string]any{"id": "t1"}}
			}
			b, _ := json.Marshal(reply)
			_, _ = server.Write(append(b, '\n'))
		}
	}()
	c := newRPCConn(toClient, send, func(string, json.RawMessage) {}, nil)

	res, err := c.Call[struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}](context.Background(), "thread/start", nil)
	if err != nil || res.Thread.ID != "t1" {
		t.Fatalf("thread/start = %+v, %v", res, err)
	}
	if _, err := c.Call[json.RawMessage](context.Background(), "initialized", nil); err != nil {
		t.Fatalf("a response without a result: %v", err)
	}
}
