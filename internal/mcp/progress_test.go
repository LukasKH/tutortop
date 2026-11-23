package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// keepAlive sends periodic MCP progress notifications over Streamable HTTP
// while a slow tool call runs (when the client supplies a progressToken), and
// stops before the tool returns.
func TestKeepAliveSendsProgress(t *testing.T) {
	old := progressInterval
	progressInterval = 15 * time.Millisecond
	t.Cleanup(func() { progressInterval = old })

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "slow"}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		defer keepAlive(ctx, req, "building sandbox")()
		time.Sleep(60 * time.Millisecond)
		return textResult("done"), nil, nil
	})
	httpSrv := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil))
	t.Cleanup(httpSrv.Close)

	var (
		mu       sync.Mutex
		progress []*mcpsdk.ProgressNotificationParams
	)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, &mcpsdk.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcpsdk.ProgressNotificationClientRequest) {
			mu.Lock()
			progress = append(progress, req.Params)
			mu.Unlock()
		},
	})
	cs, err := client.Connect(t.Context(), &mcpsdk.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// With a progressToken, notifications arrive with rising progress.
	params := &mcpsdk.CallToolParams{Name: "slow", Arguments: map[string]any{}}
	params.SetProgressToken("tok-1")
	if _, err := cs.CallTool(t.Context(), params); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]*mcpsdk.ProgressNotificationParams(nil), progress...)
	progress = nil
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("got %d progress notifications, want at least 2", len(got))
	}
	for i, p := range got {
		if p.ProgressToken != "tok-1" || p.Message != "building sandbox" || p.Progress != float64(i+1) {
			t.Errorf("notification %d = %+v, want token tok-1, progress %d", i, p, i+1)
		}
	}

	// Without a progressToken, the tool finishes cleanly with no notifications.
	if _, err := cs.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "slow", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	extra := len(progress)
	mu.Unlock()
	if extra != 0 {
		t.Errorf("got %d notifications without a progressToken, want 0", extra)
	}
}
