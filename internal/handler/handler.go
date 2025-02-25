package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	"butterfly.orx.me/core/log"
	"github.com/gin-gonic/gin"
	"github.com/orvice/openapi-proxy/internal/config"
)

var (
	authHeader = "Authorization"

	openAIProxies = make(map[string]*httputil.ReverseProxy)
	modelsMap     = make(map[*regexp.Regexp]string)
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
		modifyRequest(req, conf, url)
	}

	proxy.ModifyResponse = modifyResponse()
	proxy.ErrorHandler = errorHandler()
	return proxy, nil
}

func modifyRequest(req *http.Request, conf config.Vendor, newURL *url.URL) {
	logger := log.FromContext(req.Context())
	//  chekc if auth header is empty
	if req.Header.Get(authHeader) == "" {
		logger.Info("no token found, using default token")
		req.Header.Set(authHeader, "Bearer "+conf.Key)
	} else {
		logger.Info("token found in request")
		bearerHeader := req.Header.Get(authHeader)
		arr := strings.Split(bearerHeader, " ")
		var key string
		if len(arr) == 2 {
			key = arr[1]
		}
		if key == "null" || strings.Contains(key, "null") {
			logger.Info(" token is null, using default token")
			req.Header.Del(authHeader)
			req.Header.Set(authHeader, "Bearer "+conf.Key)
		}
	}

	req.Host = newURL.Host
	req.URL.Host = newURL.Host
	req.Header.Set("Host", newURL.Host)
	if newURL.Path != "" {
		req.URL.Path = newURL.Path + "/chat/completions"
		logger.Info("setting request path",
			"path", req.URL.Path)
	}
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
	conf := config.Conf

	for _, v := range conf.Vendors {
		proxy, err := NewProxy(v)
		if err != nil {
			slog.Error("new proxy error", "error", err)
			continue
		}
		openAIProxies[v.Name] = proxy
	}

	for _, v := range conf.Models {
		regex, err := regexp.Compile(v.Regex)
		if err != nil {
			slog.Error("compile regex error", "error", err)
			continue
		}
		modelsMap[regex] = v.Vendor
	}

	initGeminiProxy()

	var err error
	defaultProxy, err = NewProxy(config.Conf.GetDefaultVendor())
	if err != nil {
		slog.Error("new proxy error", "error", err)
		return
	}
}

func Router(r *gin.Engine) {
	initProxies()
	r.GET("/v1/models", Models)
	r.Any("/v1/chat/completions", ChatComplections)
	r.Any("/v1beta/models/:model", geminiHandler)
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
		vendor = config.Conf.DefaultVendor
	}
	proxy, ok := openAIProxies[vendor]
	if !ok {
		defaultProxy.ServeHTTP(c.Writer, c.Request)
		return
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

type ModelList struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func Models(c *gin.Context) {
	var modelObjects []ModelObject
	for _, m := range config.Conf.Models {
		modelObjects = append(modelObjects, ModelObject{
			ID:      m.Name,
			Object:  "model",
			Created: 1686935002,
			OwnedBy: "organization-owner",
		})
	}

	response := ModelList{
		Object: "list",
		Data:   modelObjects,
	}
	c.JSON(http.StatusOK, response)
}

type completionsRequest struct {
	Model string `json:"model"`
}

func ChatComplections(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	var req completionsRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		logger.Error("bind json error",
			"error", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Restore request body for proxy
	if bodyBytes, exists := c.Get(gin.BodyBytesKey); exists {
		logger.Info("restoring request body", "len", len(bodyBytes.([]byte)))
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes.([]byte)))
	}

	var vendor = config.Conf.DefaultVendor

	for k, v := range modelsMap {
		if k.MatchString(req.Model) {
			logger.Info("model matched",
				"model", req.Model, "vendor", v)
			vendor = v
		}
	}

	logger.Info("proxy request",
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
