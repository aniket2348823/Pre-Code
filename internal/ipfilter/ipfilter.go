// Package ipfilter provides IP-based access control with allow/deny lists.
package ipfilter

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// Filter controls access based on client IP addresses.
type Filter struct {
	mu         sync.RWMutex
	allowList  []net.IPNet
	denyList   []net.IPNet
	allowAll   bool
	denyAll    bool
	trustedProxies []net.IPNet
}

// NewFilter creates a new IP filter. By default all IPs are allowed.
func NewFilter() *Filter {
	return &Filter{
		allowAll: true,
	}
}

// AllowIP adds a CIDR range or single IP to the allow list.
// When the allow list is non-empty, only listed IPs are permitted.
func (f *Filter) AllowIP(cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		// Try parsing as a single IP
		ip := net.ParseIP(cidr)
		if ip == nil {
			return err
		}
		// Convert single IP to /32 or /128 network
		if ip.To4() != nil {
			network = &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}
		} else {
			network = &net.IPNet{IP: ip.To16(), Mask: net.CIDRMask(128, 128)}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowList = append(f.allowList, *network)
	f.allowAll = false
	return nil
}

// DenyIP adds a CIDR range or single IP to the deny list.
// Denied IPs are always blocked regardless of allow list.
func (f *Filter) DenyIP(cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return err
		}
		if ip.To4() != nil {
			network = &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}
		} else {
			network = &net.IPNet{IP: ip.To16(), Mask: net.CIDRMask(128, 128)}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denyList = append(f.denyList, *network)
	f.denyAll = false
	return nil
}

// AllowAll clears the allow list and permits all IPs.
func (f *Filter) AllowAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowList = nil
	f.allowAll = true
}

// DenyAll blocks all IPs.
func (f *Filter) DenyAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denyAll = true
}

// AddTrustedProxy adds a CIDR to the trusted proxy list.
// Proxy IPs are skipped when extracting the real client IP.
func (f *Filter) AddTrustedProxy(cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return err
		}
		if ip.To4() != nil {
			network = &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}
		} else {
			network = &net.IPNet{IP: ip.To16(), Mask: net.CIDRMask(128, 128)}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trustedProxies = append(f.trustedProxies, *network)
	return nil
}

// IsAllowed checks if an IP is permitted.
func (f *Filter) IsAllowed(ip net.IP) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.denyAll {
		return false
	}

	// Check deny list first
	for _, network := range f.denyList {
		if network.Contains(ip) {
			return false
		}
	}

	// If allow list is empty, all non-denied IPs are allowed
	if f.allowAll {
		return true
	}

	// Check allow list
	for _, network := range f.allowList {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// ExtractClientIP extracts the real client IP from the request,
// skipping trusted proxy headers. Safe for concurrent use.
func (f *Filter) ExtractClientIP(r *http.Request) net.IP {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Check X-Forwarded-For first (if we trust proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		for _, part := range parts {
			ip := net.ParseIP(strings.TrimSpace(part))
			if ip != nil && !f.isTrustedProxyLocked(ip) {
				return ip
			}
		}
	}

	// Check X-Real-Ip
	xri := r.Header.Get("X-Real-Ip")
	if xri != "" {
		ip := net.ParseIP(strings.TrimSpace(xri))
		if ip != nil && !f.isTrustedProxyLocked(ip) {
			return ip
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP(r.RemoteAddr)
	}
	return net.ParseIP(host)
}

// isTrustedProxyLocked checks if IP is a trusted proxy. Caller must hold f.mu.
func (f *Filter) isTrustedProxyLocked(ip net.IP) bool {
	for _, network := range f.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Middleware returns an HTTP middleware that enforces IP filtering.
func (f *Filter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := f.ExtractClientIP(r)
		if clientIP == nil || !f.IsAllowed(clientIP) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Summary returns the current filter configuration for debugging.
func (f *Filter) Summary() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return map[string]interface{}{
		"allow_all":      f.allowAll,
		"deny_all":       f.denyAll,
		"allow_count":    len(f.allowList),
		"deny_count":     len(f.denyList),
		"proxy_count":    len(f.trustedProxies),
	}
}
