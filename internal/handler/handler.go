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

	openAIProxy *httputil.ReverseProxy
)

// NewProxy takes target host and creates a reverse proxy
func NewProxy(conf *config.Config) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(conf.OpenAIEndpoint)
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

func modifyRequest(req *http.Request, conf *config.Config) {
	//  chekc if auth header is empty
	if req.Header.Get(authHeader) == "" {
		slog.Info("no token found, using default token")
		req.Header.Set(authHeader, "Bearer "+conf.OpenAIKey)
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
			req.Header.Set(authHeader, "Bearer "+conf.OpenAIKey)
		}
	}
	newUrl, err := url.Parse(conf.OpenAIEndpoint)
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

func Router(r *gin.Engine) {
	conf, err := config.New()
	if err != nil {
		slog.Error("new config error", "error", err)
		return
	}

	slog.Info("new config", slog.Any("config", conf))
	openAIProxy, err = NewProxy(conf)
	if err != nil {
		slog.Error("new proxy error", "error", err)
		return
	}
	r.Any("/v1/chat/completions", proxy)
	r.NoRoute(proxy)
}

func proxy(c *gin.Context) {
	slog.Info("proxy request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"path", c.Request.URL.Path)
	openAIProxy.ServeHTTP(c.Writer, c.Request)
}

func ChatComplections(c *gin.Context) {
}
