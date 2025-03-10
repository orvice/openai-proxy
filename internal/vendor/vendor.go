package vendor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"butterfly.orx.me/core/log"
	"github.com/orvice/openapi-proxy/internal/config"
)

type Vender struct {
	conf      config.Vendor
	validKeys []string
	mutex     sync.RWMutex
}

// Global random source with proper seeding
var (
	rng      = rand.New(rand.NewSource(time.Now().UnixNano()))
	rngMutex sync.Mutex
)

func NewVender(conf config.Vendor) *Vender {
	slog.Info("Creating new vendor instance", "vendor", conf.Name, "host", conf.Host)

	v := &Vender{
		conf:      conf,
		validKeys: make([]string, 0),
	}

	// Initialize by checking all keys
	v.RefreshValidKeys()

	// Start a goroutine to periodically check keys
	go v.periodicKeyCheck()

	slog.Info("Vendor instance created successfully", "vendor", conf.Name)
	return v
}

// maskKey returns a masked version of the API key for logging purposes
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}

	return key[:4] + "..." + key[len(key)-4:]
}

func (v *Vender) ReverseProxy() (*httputil.ReverseProxy, error) {
	slog.Debug("Creating reverse proxy", "vendor", v.conf.Name, "host", v.conf.Host)

	// Parse the host URL
	url, err := url.Parse(v.conf.Host)
	if err != nil {
		slog.Error("Failed to parse vendor host", "vendor", v.conf.Name, "host", v.conf.Host, "error", err)
		return nil, fmt.Errorf("failed to parse vendor host: %w", err)
	}

	// Create a new reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(url)

	// Store the original director for request modification
	originalDirector := proxy.Director

	// Create a new director function that modifies the request
	proxy.Director = func(req *http.Request) {
		// Call the original director first
		originalDirector(req)

		// Modify the request
		v.modifyProxyRequest(req, url)
	}

	// Set up response modification (if needed)
	proxy.ModifyResponse = func(resp *http.Response) error {
		slog.Debug("Proxy response received",
			"vendor", v.conf.Name,
			"status", resp.StatusCode,
			"content_length", resp.ContentLength)
		return nil // No modifications needed for now
	}

	// Set up error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		slog.Error("Proxy error occurred",
			"vendor", v.conf.Name,
			"path", req.URL.Path,
			"method", req.Method,
			"error", err)

		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(fmt.Sprintf(`{"error": {"message": "Proxy error: %v", "type": "proxy_error"}}`, err)))
	}

	slog.Info("Reverse proxy created successfully", "vendor", v.conf.Name, "target_host", url.Host)
	return proxy, nil
}

// RefreshValidKeys checks all keys in the keys list and updates the validKeys slice
func (v *Vender) RefreshValidKeys() {
	slog.Info("Refreshing valid keys", "vendor", v.conf.Name, "vendor_type", v.GetVendorType())

	// Collect all keys to check
	keysToCheck := make([]string, 0)

	// Add the main key if not empty
	if v.conf.Key != "" {
		keysToCheck = append(keysToCheck, v.conf.Key)
	}

	// Add additional keys from the list
	keysToCheck = append(keysToCheck, v.conf.Keys...)

	// Check each key and collect valid ones
	validKeys := make([]string, 0)
	for _, key := range keysToCheck {
		if key == "" {
			continue // Skip empty keys
		}

		isValid, err := v.checkKey(key)

		// Create a masked key for logging (show only first 4 and last 4 characters)
		maskedKey := maskKey(key)

		if err != nil {
			slog.Error("Error checking API key",
				"vendor", v.conf.Name,
				"key", maskedKey,
				"error", err)
			continue
		}

		if isValid {
			validKeys = append(validKeys, key)
			slog.Debug("Valid API key found", "vendor", v.conf.Name, "key", maskedKey)
		} else {
			slog.Warn("Invalid API key detected", "vendor", v.conf.Name, "key", maskedKey)
		}
	}

	// Update the valid keys list with lock protection
	v.mutex.Lock()
	v.validKeys = validKeys
	v.mutex.Unlock()

	slog.Info("Completed refreshing valid keys",
		"vendor", v.conf.Name,
		"valid_keys", len(validKeys),
		"total_keys", len(keysToCheck))
}

// periodicKeyCheck runs a background routine to check keys periodically
func (v *Vender) periodicKeyCheck() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	slog.Info("Started periodic key check for vendor", "vendor", v.conf.Name, "interval", "15m")

	for range ticker.C {
		v.RefreshValidKeys()
	}
}

func (v *Vender) GetHost() string {
	slog.Debug("Getting vendor host", "vendor", v.conf.Name, "host", v.conf.Host)
	return v.conf.Host
}

// modifyProxyRequest modifies the incoming request before forwarding it to the target host
func (v *Vender) modifyProxyRequest(req *http.Request, targetURL *url.URL) {
	slog.Debug("Modifying proxy request",
		"vendor", v.conf.Name,
		"method", req.Method,
		"path", req.URL.Path,
		"target_host", targetURL.Host)

	// Check if authorization header is empty or contains "null"
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" || strings.Contains(authHeader, "null") {
		// Use a valid API key from our validated keys
		key := v.GetKey()
		maskedKey := maskKey(key)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
		slog.Info("Using vendor API key for request",
			"vendor", v.conf.Name,
			"path", req.URL.Path,
			"key", maskedKey)
	} else {
		slog.Debug("Using provided authorization header", "vendor", v.conf.Name)
	}

	// Set the proper host
	req.Host = targetURL.Host
	req.URL.Host = targetURL.Host
	req.Header.Set("Host", targetURL.Host)

	// Adjust the path if needed based on vendor type
	if v.GetVendorType() == VendorTypeSiliconFlow && !strings.Contains(req.URL.Path, "/chat/completions") {
		// If the path doesn't already have /chat/completions and it's SiliconFlow,
		// we'll add it (following the pattern seen in the handler)
		if targetURL.Path != "" {
			originalPath := req.URL.Path
			req.URL.Path = targetURL.Path + "/chat/completions"
			slog.Info("Adjusted request path for SiliconFlow",
				"vendor", v.conf.Name,
				"original_path", originalPath,
				"new_path", req.URL.Path)
		}
	}

	slog.Debug("Request modification complete",
		"vendor", v.conf.Name,
		"final_path", req.URL.Path,
		"final_host", req.Host)
}

func (v *Vender) GetKey() string {
	v.mutex.RLock()
	defer v.mutex.RUnlock()

	// If no additional keys are configured, return the main key
	if len(v.conf.Keys) == 0 {
		return v.conf.Key
	}

	// If no valid keys are available, return the main key even if it's invalid
	if len(v.validKeys) == 0 {
		slog.Warn("No valid keys available, using default key",
			"vendor", v.conf.Name,
			"key", maskKey(v.conf.Key))
		return v.conf.Key
	}

	// Return a random valid key using the thread-safe approach
	rngMutex.Lock()
	index := rng.Intn(len(v.validKeys))
	selectedKey := v.validKeys[index]
	rngMutex.Unlock()

	slog.Debug("Selected valid key for request",
		"vendor", v.conf.Name,
		"key", maskKey(selectedKey),
		"valid_key_count", len(v.validKeys))

	return selectedKey
}

// ShouldHideModels returns true if this vendor's models should be hidden
func (v *Vender) ShouldHideModels() bool {
	return v.conf.HideModels
}

// VendorType represents the type of API vendor
type VendorType string

const (
	VendorTypeOpenAI      VendorType = "openai"
	VendorTypeSiliconFlow VendorType = "siliconflow"
)

// Returns the vendor type based on the host
func (v *Vender) GetVendorType() VendorType {
	host := strings.ToLower(v.conf.Host)
	var vendorType VendorType

	if strings.Contains(host, "siliconflow") {
		vendorType = VendorTypeSiliconFlow
	} else {
		vendorType = VendorTypeOpenAI
	}

	slog.Debug("Determined vendor type", "vendor", v.conf.Name, "host", host, "type", vendorType)
	return vendorType
}

func (v *Vender) checkKey(key string) (bool, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Create a new HTTP client
	client := &http.Client{}

	// Determine the vendor type and use appropriate validation method
	vendorType := v.GetVendorType()
	switch vendorType {
	case VendorTypeSiliconFlow:
		return v.checkSiliconFlowKey(ctx, client, key)
	case VendorTypeOpenAI:
		fallthrough
	default:
		return v.checkOpenAIKey(ctx, client, key)
	}
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

// Models calls the /v1/models endpoint and returns the models list
func (v *Vender) Models(ctx context.Context) (*ModelList, error) {
	logger := log.FromContext(ctx)
	logger.Info("Fetching models list",
		"vendor", v.conf.Name,
		"host", v.conf.Host)

	// Create a new HTTP client
	client := &http.Client{}

	// Determine the base URL based on vendor type
	baseURL := v.conf.Host

	// Prepare the request to the models endpoint with context
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		logger.Error("Failed to create models request",
			"vendor", v.conf.Name,
			"error", err)
		return nil, fmt.Errorf("error creating models request: %w", err)
	}

	// Set the API key in the Authorization header
	key := v.GetKey()
	logger.Debug("Using API key for models request",
		"vendor", v.conf.Name,
		"key", maskKey(key))
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", key))

	// Send the request
	logger.Debug("Sending models request",
		"vendor", v.conf.Name,
		"url", baseURL+"/v1/models")
	res, err := client.Do(req)
	if err != nil {
		logger.Error("Failed to make models request",
			"vendor", v.conf.Name,
			"error", err)
		return nil, fmt.Errorf("error making models request: %w", err)
	}
	defer res.Body.Close()

	// Read the response body
	body, err := io.ReadAll(res.Body)
	if err != nil {
		logger.Error("Failed to read models response",
			"vendor", v.conf.Name,
			"error", err)
		return nil, fmt.Errorf("error reading models response: %w", err)
	}

	// Check if the response status code indicates success
	if res.StatusCode != http.StatusOK {
		logger.Debug("Models request failed",
			"vendor", v.conf.Name,
			"status", res.StatusCode,
			"response", string(body))
		return nil, fmt.Errorf("models request failed with status %d: %s", res.StatusCode, string(body))
	}

	logger.Debug("Models request successful",
		"vendor", v.conf.Name,
		"status", res.StatusCode,
		"response_size", len(body))

	// Parse the response into a ModelList struct
	var modelList ModelList
	if err := json.Unmarshal(body, &modelList); err != nil {
		logger.Error("Failed to parse models response",
			"vendor", v.conf.Name,
			"error", err,
			"response", string(body))
		return nil, fmt.Errorf("error parsing models response: %w", err)
	}

	logger.Info("Successfully fetched models list",
		"vendor", v.conf.Name,
		"model_count", len(modelList.Data))

	return &modelList, nil
}

// checkOpenAIKey validates an OpenAI API key
func (v *Vender) checkOpenAIKey(ctx context.Context, client *http.Client, key string) (bool, error) {
	// Prepare the request to the models endpoint with context
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return false, fmt.Errorf("error creating OpenAI request: %w", err)
	}

	// Set the API key in the Authorization header
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", key))

	// Send the request
	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("error making OpenAI request: %w", err)
	}
	defer res.Body.Close()

	// Check if the response status code indicates success
	if res.StatusCode == http.StatusOK {
		return true, nil
	}

	// Read the error response for debugging purposes
	body, _ := io.ReadAll(res.Body)
	slog.Debug("OpenAI API key validation failed",
		"vendor", v.conf.Name,
		"status", res.StatusCode,
		"response", string(body))

	return false, nil // Key is invalid, but not an error
}

// SiliconFlowUserInfo represents the response structure from SiliconFlow API
type SiliconFlowUserInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  bool   `json:"status"`
	Data    struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Email         string `json:"email"`
		IsAdmin       bool   `json:"isAdmin"`
		Balance       string `json:"balance"`
		Status        string `json:"status"`
		ChargeBalance string `json:"chargeBalance"`
		TotalBalance  string `json:"totalBalance"`
	} `json:"data"`
}

// checkSiliconFlowKey validates a SiliconFlow API key by checking account balance
func (v *Vender) checkSiliconFlowKey(ctx context.Context, client *http.Client, key string) (bool, error) {
	// Prepare the request to the user info endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.siliconflow.cn/v1/user/info", nil)
	if err != nil {
		return false, fmt.Errorf("error creating SiliconFlow request: %w", err)
	}

	// Set the API key in the Authorization header
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", key))

	// Send the request
	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("error making SiliconFlow request: %w", err)
	}
	defer res.Body.Close()

	// If not successful response, key is invalid
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		slog.Debug("SiliconFlow API key validation failed",
			"vendor", v.conf.Name,
			"status", res.StatusCode,
			"response", string(body))
		return false, nil
	}

	// Parse the response body
	var userInfo SiliconFlowUserInfo
	body, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return false, fmt.Errorf("error parsing SiliconFlow response: %w", err)
	}

	// Check account status and balance
	if !userInfo.Status || userInfo.Data.Status != "normal" {
		slog.Debug("SiliconFlow account status is not normal",
			"vendor", v.conf.Name,
			"status", userInfo.Data.Status)
		return false, nil
	}

	// Convert total balance to float and check if greater than 0
	totalBalance, err := strconv.ParseFloat(userInfo.Data.TotalBalance, 64)
	if err != nil {
		return false, fmt.Errorf("error parsing SiliconFlow total balance: %w", err)
	}

	// Check if balance is sufficient
	if totalBalance <= 0 {
		slog.Debug("SiliconFlow account has insufficient balance",
			"vendor", v.conf.Name,
			"balance", userInfo.Data.TotalBalance)
		return false, nil
	}

	return true, nil
}
