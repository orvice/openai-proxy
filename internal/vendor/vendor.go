package vendor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

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
	v := &Vender{
		conf:      conf,
		validKeys: make([]string, 0),
	}

	// Initialize by checking all keys
	v.RefreshValidKeys()

	// Start a goroutine to periodically check keys
	go v.periodicKeyCheck()

	return v
}

func (v *Vender) ReverseProxy() (*httputil.ReverseProxy, error) {
	// Parse the host URL
	url, err := url.Parse(v.conf.Host)
	if err != nil {
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
		return nil // No modifications needed for now
	}

	// Set up error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		fmt.Printf("Proxy error: %v\n", err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(fmt.Sprintf(`{"error": {"message": "Proxy error: %v", "type": "proxy_error"}}`, err)))
	}

	return proxy, nil
}

// RefreshValidKeys checks all keys in the keys list and updates the validKeys slice
func (v *Vender) RefreshValidKeys() {
	// Check the main key first
	validKeys := make([]string, 0)
	if v.conf.Key != "" && v.checkKey(v.conf.Key) {
		validKeys = append(validKeys, v.conf.Key)
	}

	// Check all keys in the keys list
	for _, key := range v.conf.Keys {
		if key != "" && v.checkKey(key) {
			validKeys = append(validKeys, key)
		}
	}

	// Update the valid keys list with lock protection
	v.mutex.Lock()
	v.validKeys = validKeys
	v.mutex.Unlock()

	fmt.Printf("Found %d valid API keys\n", len(validKeys))
}

// periodicKeyCheck runs a background routine to check keys periodically
func (v *Vender) periodicKeyCheck() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		v.RefreshValidKeys()
	}
}

func (v *Vender) GetHost() string {
	return v.conf.Host
}

// modifyProxyRequest modifies the incoming request before forwarding it to the target host
func (v *Vender) modifyProxyRequest(req *http.Request, targetURL *url.URL) {
	// Check if authorization header is empty or contains "null"
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" || strings.Contains(authHeader, "null") {
		// Use a valid API key from our validated keys
		key := v.GetKey()
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
		fmt.Printf("Using vendor API key for request to %s\n", req.URL.Path)
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
			req.URL.Path = targetURL.Path + "/chat/completions"
		}
	}
}

func (v *Vender) GetKey() string {
	v.mutex.RLock()
	defer v.mutex.RUnlock()

	// If no valid keys are available, return the main key even if it's invalid
	if len(v.validKeys) == 0 {
		return v.conf.Key
	}

	// Return a random valid key using the thread-safe approach
	rngMutex.Lock()
	index := rng.Intn(len(v.validKeys))
	rngMutex.Unlock()

	return v.validKeys[index]
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
	if strings.Contains(host, "siliconflow") {
		return VendorTypeSiliconFlow
	}
	return VendorTypeOpenAI
}

func (v *Vender) checkKey(key string) bool {
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

// checkOpenAIKey validates an OpenAI API key
func (v *Vender) checkOpenAIKey(ctx context.Context, client *http.Client, key string) bool {
	// Prepare the request to the models endpoint with context
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		fmt.Printf("Error creating OpenAI request: %v\n", err)
		return false
	}

	// Set the API key in the Authorization header
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", key))

	// Send the request
	res, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making OpenAI request: %v\n", err)
		return false
	}
	defer res.Body.Close()

	// Check if the response status code indicates success
	if res.StatusCode == http.StatusOK {
		return true
	}

	// Read the error response for debugging purposes
	body, _ := io.ReadAll(res.Body)
	fmt.Printf("OpenAI API key validation failed with status %d: %s\n", res.StatusCode, string(body))

	return false
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
func (v *Vender) checkSiliconFlowKey(ctx context.Context, client *http.Client, key string) bool {
	// Prepare the request to the user info endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.siliconflow.cn/v1/user/info", nil)
	if err != nil {
		fmt.Printf("Error creating SiliconFlow request: %v\n", err)
		return false
	}

	// Set the API key in the Authorization header
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", key))

	// Send the request
	res, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making SiliconFlow request: %v\n", err)
		return false
	}
	defer res.Body.Close()

	// If not successful response, key is invalid
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		fmt.Printf("SiliconFlow API key validation failed with status %d: %s\n", res.StatusCode, string(body))
		return false
	}

	// Parse the response body
	var userInfo SiliconFlowUserInfo
	body, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(body, &userInfo); err != nil {
		fmt.Printf("Error parsing SiliconFlow response: %v\n", err)
		return false
	}

	// Check account status and balance
	if !userInfo.Status || userInfo.Data.Status != "normal" {
		fmt.Printf("SiliconFlow account status is not normal: %s\n", userInfo.Data.Status)
		return false
	}

	// Convert total balance to float and check if greater than 0
	totalBalance, err := strconv.ParseFloat(userInfo.Data.TotalBalance, 64)
	if err != nil {
		fmt.Printf("Error parsing SiliconFlow total balance: %v\n", err)
		return false
	}

	// Check if balance is sufficient
	if totalBalance <= 0 {
		fmt.Printf("SiliconFlow account has insufficient balance: %s\n", userInfo.Data.TotalBalance)
		return false
	}

	return true
}
