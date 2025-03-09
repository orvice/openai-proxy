package vendor

import (
	"log/slog"
	"net/http/httputil"
	"regexp"
	"sync"

	"github.com/orvice/openapi-proxy/internal/config"
)

type VendorManager struct {
	// Map of vendor name to vendor instance
	Vendors map[string]*Vender
	
	// Map of vendor name to proxy
	Proxies map[string]*httputil.ReverseProxy
	
	// Map of model regex to vendor name
	ModelsMap map[*regexp.Regexp]string
	
	// Default proxy to use when no match is found
	DefaultProxy *httputil.ReverseProxy
	
	// Default vendor
	DefaultVendor *Vender
	
	// Lock for concurrent access
	mutex sync.RWMutex
	
	// Configuration
	conf *config.Config
}

// NewVendorManager creates a new VendorManager with the given configuration
func NewVendorManager(conf *config.Config) *VendorManager {
	return &VendorManager{
		Vendors:   make(map[string]*Vender),
		Proxies:   make(map[string]*httputil.ReverseProxy),
		ModelsMap: make(map[*regexp.Regexp]string),
		conf:      conf,
	}
}

// Initialize sets up all proxies and model mappings
func (m *VendorManager) Initialize() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Initialize vendor proxies
	for _, v := range m.conf.Vendors {
		// Create vendor instance
		vender := NewVender(v)
		m.Vendors[v.Name] = vender
		
		// Create reverse proxy
		proxy, err := vender.ReverseProxy()
		if err != nil {
			slog.Error("failed to create proxy for vendor", 
				"vendor", v.Name, 
				"error", err)
			continue
		}
		m.Proxies[v.Name] = proxy
		slog.Info("initialized proxy for vendor", "vendor", v.Name)
	}

	// Initialize model to vendor mappings
	for _, model := range m.conf.Models {
		regex, err := regexp.Compile(model.Regex)
		if err != nil {
			slog.Error("failed to compile regex for model", 
				"model", model.Name, 
				"regex", model.Regex, 
				"error", err)
			continue
		}
		m.ModelsMap[regex] = model.Vendor
		slog.Info("mapped model to vendor", 
			"model", model.Name, 
			"regex", model.Regex, 
			"vendor", model.Vendor)
	}

	// Initialize default vendor and proxy
	defaultVendorConf := m.conf.GetDefaultVendor()
	m.DefaultVendor = NewVender(defaultVendorConf)
	
	var err error
	m.DefaultProxy, err = m.DefaultVendor.ReverseProxy()
	if err != nil {
		slog.Error("failed to create default proxy", "error", err)
		return err
	}
	slog.Info("initialized default proxy")
	
	return nil
}

// Get a vendor instance by name
func (m *VendorManager) GetVendor(name string) *Vender {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if name == "" {
		name = m.conf.DefaultVendor
	}
	
	vendor, exists := m.Vendors[name]
	if !exists {
		return m.DefaultVendor
	}
	
	return vendor
}

// GetProxyForVendor returns the proxy for the given vendor name
func (m *VendorManager) GetProxyForVendor(vendorName string) *httputil.ReverseProxy {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if vendorName == "" {
		vendorName = m.conf.DefaultVendor
	}
	
	proxy, exists := m.Proxies[vendorName]
	if !exists {
		return m.DefaultProxy
	}
	
	return proxy
}

// GetVendorForModel returns the vendor name for the given model
func (m *VendorManager) GetVendorForModel(modelName string) string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	// Default to the configured default vendor
	vendor := m.conf.DefaultVendor
	
	// Check if any regex patterns match the model name
	for regex, vendorName := range m.ModelsMap {
		if regex.MatchString(modelName) {
			vendor = vendorName
			break
		}
	}
	
	return vendor
}

// GetProxyForModel returns the proxy for the given model
func (m *VendorManager) GetProxyForModel(modelName string) *httputil.ReverseProxy {
	vendorName := m.GetVendorForModel(modelName)
	return m.GetProxyForVendor(vendorName)
}
