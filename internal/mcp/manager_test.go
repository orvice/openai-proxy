package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orvice/aiproxy/internal/config"
)

func TestReverseProxyStripsRoutePrefixAndInjectsHeaders(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotHeader string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-Test-Header")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	proxy, err := newReverseProxy(config.MCPServer{
		Name: upstream.URL,
		Host: upstream.URL + "/sse",
		Key:  "secret-key",
		Headers: map[string]string{
			"X-Test-Header": "hello",
		},
	})
	if err != nil {
		t.Fatalf("newReverseProxy() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp/demo/tools/list", nil)
	req.Header.Set(routePrefixHeader, "/mcp/demo")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if gotPath != "/sse/tools/list" {
		t.Fatalf("forwarded path = %q, want %q", gotPath, "/sse/tools/list")
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("authorization = %q, want %q", gotAuth, "Bearer secret-key")
	}
	if gotHeader != "hello" {
		t.Fatalf("x-test-header = %q, want %q", gotHeader, "hello")
	}
}

func TestResolveServerNameFallsBackToDefault(t *testing.T) {
	t.Parallel()

	manager := NewManager(&config.Config{
		DefaultMCPServer: "demo",
		MCPServers: []config.MCPServer{
			{Name: "demo", Host: "https://example.com/mcp"},
		},
	})
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	name, ok := manager.ResolveServerName("")
	if !ok {
		t.Fatal("ResolveServerName(\"\") returned ok=false")
	}
	if name != "demo" {
		t.Fatalf("ResolveServerName(\"\") = %q, want %q", name, "demo")
	}
}
