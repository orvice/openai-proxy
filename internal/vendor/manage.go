package vendor

import (
	"log/slog"
	"net/http/httputil"
	"regexp"
	"sync"
	"time"

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
	slog.Info("Creating new VendorManager instance")
	
	manager := &VendorManager{
		Vendors:   make(map[string]*Vender),
		Proxies:   make(map[string]*httputil.ReverseProxy),
		ModelsMap: make(map[*regexp.Regexp]string),
		conf:      conf,
	}
	
	slog.Debug("VendorManager instance created")
	return manager
}

// Initialize sets up all proxies and model mappings
func (m *VendorManager) Initialize() error {
	slog.Info("Initializing VendorManager", "vendor_count", len(m.conf.Vendors), "model_count", len(m.conf.Models))
	
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Initialize vendor proxies
	for _, v := range m.conf.Vendors {
		slog.Debug("Setting up vendor", "vendor", v.Name, "host", v.Host)
		
		// Create vendor instance
		vender := NewVender(v)
		m.Vendors[v.Name] = vender
		
		// Create reverse proxy
		proxy, err := vender.ReverseProxy()
		if err != nil {
			slog.Error("Failed to create proxy for vendor", 
				"vendor", v.Name, 
				"error", err)
			continue
		}
		m.Proxies[v.Name] = proxy
		slog.Info("Initialized proxy for vendor", "vendor", v.Name)
	}

	// Initialize model to vendor mappings
	for _, model := range m.conf.Models {
		slog.Debug("Setting up model mapping", "model", model.Name, "regex", model.Regex, "vendor", model.Vendor)
		
		regex, err := regexp.Compile(model.Regex)
		if err != nil {
			slog.Error("Failed to compile regex for model", 
				"model", model.Name, 
				"regex", model.Regex, 
				"error", err)
			continue
		}
		m.ModelsMap[regex] = model.Vendor
		slog.Info("Mapped model to vendor", 
			"model", model.Name, 
			"regex", model.Regex, 
			"vendor", model.Vendor)
	}

	// Initialize default vendor and proxy
	slog.Debug("Setting up default vendor", "default_vendor", m.conf.DefaultVendor)
	defaultVendorConf := m.conf.GetDefaultVendor()
	m.DefaultVendor = NewVender(defaultVendorConf)
	
	var err error
	m.DefaultProxy, err = m.DefaultVendor.ReverseProxy()
	if err != nil {
		slog.Error("Failed to create default proxy", "error", err)
		return err
	}
	slog.Info("Initialized default proxy", "vendor", defaultVendorConf.Name)
	
	// Start the periodic key validation task
	go m.StartRefreshKeysTask()
	
	slog.Info("VendorManager initialization complete", 
		"vendors_count", len(m.Vendors), 
		"models_count", len(m.ModelsMap))
	return nil
}

// Get a vendor instance by name
func (m *VendorManager) GetVendor(name string) *Vender {
	slog.Debug("Getting vendor by name", "requested_vendor", name)
	
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if name == "" {
		name = m.conf.DefaultVendor
		slog.Debug("No vendor specified, using default", "default_vendor", name)
	}
	
	vendor, exists := m.Vendors[name]
	if !exists {
		slog.Debug("Vendor not found, using default", "requested_vendor", name, "default_vendor", m.conf.DefaultVendor)
		return m.DefaultVendor
	}
	
	slog.Debug("Vendor found", "vendor", name)
	return vendor
}

// GetProxyForVendor returns the proxy for the given vendor name
func (m *VendorManager) GetProxyForVendor(vendorName string) *httputil.ReverseProxy {
	slog.Debug("Getting proxy for vendor", "requested_vendor", vendorName)
	
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if vendorName == "" {
		vendorName = m.conf.DefaultVendor
		slog.Debug("No vendor specified, using default", "default_vendor", vendorName)
	}
	
	proxy, exists := m.Proxies[vendorName]
	if !exists {
		slog.Debug("Proxy not found for vendor, using default", 
			"requested_vendor", vendorName, 
			"default_vendor", m.conf.DefaultVendor)
		return m.DefaultProxy
	}
	
	slog.Debug("Proxy found for vendor", "vendor", vendorName)
	return proxy
}

// GetVendorForModel returns the vendor name for the given model
func (m *VendorManager) GetVendorForModel(modelName string) string {
	slog.Debug("Getting vendor for model", "model", modelName)
	
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	// Default to the configured default vendor
	vendor := m.conf.DefaultVendor
	
	// Check if any regex patterns match the model name
	for pattern, vName := range m.ModelsMap {
		if pattern.MatchString(modelName) {
			vendor = vName
			slog.Debug("Model matched pattern", 
				"model", modelName, 
				"pattern", pattern.String(), 
				"vendor", vName)
			break
		}
	}
	
	slog.Info("Selected vendor for model", "model", modelName, "vendor", vendor)
	return vendor
}

// GetProxyForModel returns the proxy for the given model
func (m *VendorManager) GetProxyForModel(modelName string) *httputil.ReverseProxy {
	slog.Debug("Getting proxy for model", "model", modelName)
	
	vendorName := m.GetVendorForModel(modelName)
	slog.Debug("Using vendor for model", "model", modelName, "vendor", vendorName)
	
	return m.GetProxyForVendor(vendorName)
}

// RefreshAllKeys refreshes the valid keys for all vendors
func (m *VendorManager) RefreshAllKeys() {
	slog.Info("Starting validation for all vendor keys")
	
	m.mutex.RLock()
	vendors := make([]*Vender, 0, len(m.Vendors))
	
	// Collect all vendors first to avoid holding the lock during key validation
	for name, vender := range m.Vendors {
		slog.Debug("Adding vendor to validation list", "vendor", name)
		vendors = append(vendors, vender)
	}
	
	// Add default vendor if it's not already included
	defaultVendorIncluded := false
	for _, v := range vendors {
		if v == m.DefaultVendor {
			defaultVendorIncluded = true
			break
		}
	}
	if !defaultVendorIncluded && m.DefaultVendor != nil {
		slog.Debug("Adding default vendor to validation list", "vendor", m.DefaultVendor.conf.Name)
		vendors = append(vendors, m.DefaultVendor)
	}
	m.mutex.RUnlock()

	// Refresh keys for each vendor
	slog.Info("Beginning validation for all vendor keys", "vendor_count", len(vendors))
	for _, vender := range vendors {
		slog.Debug("Refreshing keys for vendor", "vendor", vender.conf.Name)
		vender.RefreshValidKeys()
	}
	slog.Info("Completed validation for all vendor keys", "vendor_count", len(vendors))
}

// StartRefreshKeysTask starts a background task to periodically refresh all vendor keys
func (m *VendorManager) StartRefreshKeysTask() {
	slog.Info("Starting periodic key refresh task")
	
	// Initial refresh immediately after startup
	slog.Debug("Performing initial key validation")
	m.RefreshAllKeys()
	
	// Set up a ticker to periodically refresh all keys
	interval := 10 * time.Minute
	ticker := time.NewTicker(interval)
	slog.Info("Started scheduled task for refreshing vendor API keys", 
		"interval_minutes", interval.Minutes(), 
		"next_run", time.Now().Add(interval))
	
	go func() {
		defer ticker.Stop()
		for t := range ticker.C {
			slog.Info("Running scheduled key validation", "time", t)
			m.RefreshAllKeys()
			slog.Debug("Next scheduled validation at", "time", t.Add(interval))
		}
	}()
	
	slog.Info("Periodic key refresh task started successfully")
}
