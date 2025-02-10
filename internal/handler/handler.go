package handler

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/orvice/openapi-proxy/internal/config"
)

var (
	authHeader = "Authorization"

	openAIProxies map[string]*httputil.ReverseProxy
	defaultProxy  *httputil.ReverseProxy
)

// NewProxy takes target host and creates a reverse proxy
func NewProxy(conf config.Vendor) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(conf.Host)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(url)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		modifyRequest(req, conf)
	}

	proxy.ModifyResponse = modifyResponse()
	proxy.ErrorHandler = errorHandler()
	return proxy, nil
}

func modifyRequest(req *http.Request, conf config.Vendor) {
	//  chekc if auth header is empty
	if req.Header.Get(authHeader) == "" {
		slog.Info("no token found, using default token")
		req.Header.Set(authHeader, "Bearer "+conf.Key)
	} else {
		slog.Info("token found in request")
		bearerHeader := req.Header.Get(authHeader)
		arr := strings.Split(bearerHeader, " ")
		var key string
		if len(arr) == 2 {
			key = arr[1]
		}
		if key == "null" || strings.Contains(key, "null") {
			slog.Info(" token is null, using default token")
			req.Header.Del(authHeader)
			req.Header.Set(authHeader, "Bearer "+conf.Key)
		}
	}
	newUrl, err := url.Parse(conf.Host)
	if err != nil {
		slog.Error("parse openai endpoint error", "error", err)
		return
	}
	req.Host = newUrl.Host
	req.URL.Host = newUrl.Host
	req.Header.Set("Host", newUrl.Host)
}

func errorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, req *http.Request, err error) {
		slog.Error("Got error while modifying response", "error", err)
	}
}

func modifyResponse() func(*http.Response) error {
	return func(resp *http.Response) error {
		return nil
	}
}

func initProxies() {
	conf, err := config.New()
	if err != nil {
		slog.Error("new config error", "error", err)
		return
	}

	for _, v := range conf.Vendors {
		proxy, err := NewProxy(v)
		if err != nil {
			slog.Error("new proxy error", "error", err)
			continue
		}
		openAIProxies[v.Name] = proxy
	}

	defaultProxy, err = NewProxy(config.Get().GetDefaultVendor())
	if err != nil {
		slog.Error("new proxy error", "error", err)
		return
	}
}

func Router(r *gin.Engine) {
	initProxies()
	r.GET("/v1/models", Models)
	r.Any("/v1/chat/completions", proxy)
	r.NoRoute(proxy)
}

func proxy(c *gin.Context) {
	slog.Info("proxy request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"path", c.Request.URL.Path)

	vendor := c.Request.Header.Get("x-vendor")
	if vendor == "" {
		vendor = config.Get().DefaultVendor
	}
	proxy, ok := openAIProxies[vendor]
	if !ok {
		defaultProxy.ServeHTTP(c.Writer, c.Request)
		return
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func Models(c *gin.Context) {
	c.JSON(http.StatusOK, config.Get().Models)
}

type completionsRequest struct {
	Model string `json:"model"`
}

func ChatComplections(c *gin.Context) {
	var req completionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	var vendor = config.Get().DefaultVendor

	for _, v := range config.Get().Models {
		if v.Name == req.Model {
			vendor = v.Vendor
		}
	}

	slog.Info("proxy request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"model", req.Model,
		"vendor", vendor,
		"path", c.Request.URL.Path)

	proxy, ok := openAIProxies[vendor]
	if !ok {
		defaultProxy.ServeHTTP(c.Writer, c.Request)
		return
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
