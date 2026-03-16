package mcp

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/orvice/aiproxy/internal/config"
)

const routePrefixHeader = "X-Mcp-Route-Prefix"

type Manager struct {
	conf          *config.Config
	servers       map[string]config.MCPServer
	proxies       map[string]*httputil.ReverseProxy
	defaultServer string
	mutex         sync.RWMutex
}

func NewManager(conf *config.Config) *Manager {
	return &Manager{
		conf:    conf,
		servers: make(map[string]config.MCPServer),
		proxies: make(map[string]*httputil.ReverseProxy),
	}
}

func (m *Manager) Initialize() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.servers = make(map[string]config.MCPServer, len(m.conf.MCPServers))
	m.proxies = make(map[string]*httputil.ReverseProxy, len(m.conf.MCPServers))
	m.defaultServer = m.conf.DefaultMCPServer

	for i, server := range m.conf.MCPServers {
		if server.Name == "" {
			return fmt.Errorf("mcp server at index %d has empty name", i)
		}
		if server.Host == "" {
			return fmt.Errorf("mcp server %q has empty host", server.Name)
		}

		proxy, err := newReverseProxy(server)
		if err != nil {
			return fmt.Errorf("create mcp proxy for %s: %w", server.Name, err)
		}

		m.servers[server.Name] = server
		m.proxies[server.Name] = proxy
		if m.defaultServer == "" {
			m.defaultServer = server.Name
		}
	}

	return nil
}

func (m *Manager) HasServers() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return len(m.servers) > 0
}

func (m *Manager) ListServerNames() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

func (m *Manager) ResolveServerName(name string) (string, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if name != "" {
		_, ok := m.servers[name]
		return name, ok
	}

	if m.defaultServer == "" {
		return "", false
	}

	_, ok := m.servers[m.defaultServer]
	return m.defaultServer, ok
}

func (m *Manager) GetProxy(name string) (*httputil.ReverseProxy, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	proxy, ok := m.proxies[name]
	return proxy, ok
}

func newReverseProxy(server config.MCPServer) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(server.Host)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		routePrefix := req.Header.Get(routePrefixHeader)
		incomingPath := req.URL.Path
		originalDirector(req)
		req.URL.Path = joinURLPath(target.Path, stripRoutePrefix(incomingPath, routePrefix))
		req.URL.RawPath = req.URL.Path
		req.Host = target.Host
		req.Header.Set("Host", target.Host)
		req.Header.Del(routePrefixHeader)

		authHeader := req.Header.Get("Authorization")
		if (authHeader == "" || strings.Contains(strings.ToLower(authHeader), "null")) && server.Key != "" {
			req.Header.Set("Authorization", "Bearer "+server.Key)
		}
		for key, value := range server.Headers {
			req.Header.Set(key, value)
		}
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		slog.Error("mcp proxy error",
			"server", server.Name,
			"method", req.Method,
			"path", req.URL.Path,
			"error", err)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":"MCP proxy error: %v","type":"proxy_error"}}`, err)))
	}

	return proxy, nil
}

func stripRoutePrefix(path, routePrefix string) string {
	if routePrefix == "" {
		return path
	}

	trimmed := strings.TrimPrefix(path, routePrefix)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}

func joinURLPath(basePath, requestPath string) string {
	switch {
	case basePath == "":
		if requestPath == "" {
			return "/"
		}
		return requestPath
	case requestPath == "":
		return basePath
	case strings.HasSuffix(basePath, "/") && strings.HasPrefix(requestPath, "/"):
		return basePath + strings.TrimPrefix(requestPath, "/")
	case !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(requestPath, "/"):
		return basePath + "/" + requestPath
	default:
		return basePath + requestPath
	}
}
