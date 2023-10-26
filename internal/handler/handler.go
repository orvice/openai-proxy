package handler

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	defaultToken  string
	openAIApiAddr = "https://api.openai.com"
	authHeader    = "Authorization"
	openaiProxy   *httputil.ReverseProxy
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
		slog.Info("no token found, using default token")
		req.Header.Set(authHeader, "Bearer "+defaultToken)
	} else {
		slog.Info("token found in request")
		authHeader := req.Header.Get(authHeader)
		arr := strings.Split(authHeader, " ")
		var key string
		if len(arr) == 2 {
			key = arr[1]
		}
		if key == "null" {
			slog.Info(" token is null, using default token")
			req.Header.Del(authHeader)
			req.Header.Set(authHeader, "Bearer "+defaultToken)
		}
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

func Router(r *gin.Engine) {
	r.GET("/v1/chat/completions", proxy)
	r.NoRoute(proxy)
}

func Init() {
	defaultToken = os.Getenv("OPENAI_API_KEY")
	proxy, err := NewProxy(openAIApiAddr)
	if err != nil {
		slog.Error("new proxy error", "error", err)
		return
	}
	openaiProxy = proxy
}

func proxy(c *gin.Context) {
	slog.Info("proxy request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"path", c.Request.URL.Path)
	openaiProxy.ServeHTTP(c.Writer, c.Request)
}

func chatComplections(c *gin.Context) {

}
