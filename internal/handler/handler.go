package handler

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
)

var (
	defaultToken  string
	openAIApiAddr = "https://api.openai.com"
	authHeader    = "Authorization"
)

// NewProxy takes target host and creates a reverse proxy
func NewProxy(targetHost string) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(targetHost)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(url)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		modifyRequest(req)
	}

	proxy.ModifyResponse = modifyResponse()
	proxy.ErrorHandler = errorHandler()
	return proxy, nil
}

func modifyRequest(req *http.Request) {
	//  chekc if auth header is empty
	if req.Header.Get(authHeader) == "" {
		req.Header.Set(authHeader, "Bearer "+defaultToken)
	}
	req.Host = "api.openai.com"
	req.Header.Set("Host", "api.openai.com")
}

func errorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, req *http.Request, err error) {
		slog.Error("Got error while modifying response", "error", err)
		return
	}
}

func modifyResponse() func(*http.Response) error {
	return func(resp *http.Response) error {
		return nil
	}
}

// ProxyRequestHandler handles the http request using proxy
func ProxyRequestHandler(proxy *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("proxy request",
			"CF-Connecting-IP", r.Header.Get("CF-Connecting-IP"),
			"ua", r.UserAgent(),
			"method", r.Method,
			"path", r.URL.Path)
		proxy.ServeHTTP(w, r)
	}
}

func Router(r *gin.Engine) {
	r.GET("/v1/chat/completions", proxy)
	r.NoRoute(proxy)
}

func Init() {
	defaultToken = os.Getenv("OPENAI_API_KEY")
}

func proxy(c *gin.Context) {
	// initialize a reverse proxy and pass the actual backend server url here
	proxy, err := NewProxy(openAIApiAddr)
	if err != nil {
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
