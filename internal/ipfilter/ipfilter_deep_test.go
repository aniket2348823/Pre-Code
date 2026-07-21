package ipfilter

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestExtractClientIP_XFFChain(t *testing.T) {
	f := NewFilter()
	f.AddTrustedProxy("10.0.0.0/8")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2, 203.0.113.5")
	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "203.0.113.5" {
		t.Errorf("expected 203.0.113.5, got %v", ip)
	}
}

func TestExtractClientIP_IPv6(t *testing.T) {
	f := NewFilter()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::1]:8080"
	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "::1" {
		t.Errorf("expected ::1, got %v", ip)
	}
}

func TestExtractClientIP_IPv6Mapped(t *testing.T) {
	f := NewFilter()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::ffff:192.168.1.1]:8080"
	ip := f.ExtractClientIP(req)
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
}

func TestExtractClientIP_EmptyXFF(t *testing.T) {
	f := NewFilter()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:8080"
	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %v", ip)
	}
}

func TestExtractClientIP_MalformedRemoteAddr(t *testing.T) {
	f := NewFilter()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "malformed"
	// Should not panic; nil is acceptable for unparseable addresses
	_ = f.ExtractClientIP(req)
}

func TestDenyCIDR(t *testing.T) {
	f := NewFilter()
	f.DenyIP("10.0.0.0/8")
	if f.IsAllowed(net.ParseIP("10.1.2.3")) {
		t.Error("10.x should be denied")
	}
	if !f.IsAllowed(net.ParseIP("192.168.1.1")) {
		t.Error("192.168.x should be allowed")
	}
}

func TestAllowSpecificWithinDenied(t *testing.T) {
	f := NewFilter()
	f.DenyIP("10.0.0.0/8")
	f.AllowIP("10.0.0.5")
	// Deny should take precedence
	if f.IsAllowed(net.ParseIP("10.0.0.5")) {
		t.Error("deny should take precedence over allow")
	}
}

func TestDenyAll_Deep(t *testing.T) {
	f := NewFilter()
	f.DenyAll()
	if f.IsAllowed(net.ParseIP("1.2.3.4")) {
		t.Error("DenyAll should block everything")
	}
}

func TestAllowAllClearsDenyList(t *testing.T) {
	f := NewFilter()
	f.DenyIP("10.0.0.0/8")
	f.AllowAll()
	if !f.IsAllowed(net.ParseIP("10.0.0.1")) {
		t.Error("AllowAll should permit everything")
	}
}

func TestMiddleware_Blocks(t *testing.T) {
	f := NewFilter()
	f.DenyIP("192.168.0.0/16")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := f.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.10:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestMiddleware_Allows(t *testing.T) {
	f := NewFilter()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := f.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestSummary_Deep(t *testing.T) {
	f := NewFilter()
	f.AllowIP("192.168.0.0/16")
	f.DenyIP("192.168.1.0/24")
	f.AddTrustedProxy("10.0.0.0/8")
	s := f.Summary()
	if s["allow_count"] != 1 {
		t.Errorf("expected 1 allow, got %v", s["allow_count"])
	}
	if s["deny_count"] != 1 {
		t.Errorf("expected 1 deny, got %v", s["deny_count"])
	}
	if s["proxy_count"] != 1 {
		t.Errorf("expected 1 proxy, got %v", s["proxy_count"])
	}
}

func TestInvalidCIDR_Deep(t *testing.T) {
	f := NewFilter()
	if err := f.AllowIP("not-a-cidr"); err == nil {
		t.Error("expected error for invalid CIDR")
	}
}

func TestIPv6(t *testing.T) {
	f := NewFilter()
	f.DenyIP("::1/128")
	if f.IsAllowed(net.ParseIP("::1")) {
		t.Error("::1 should be denied")
	}
	if !f.IsAllowed(net.ParseIP("::2")) {
		t.Error("::2 should be allowed")
	}
}

func TestConcurrentAllowDeny(t *testing.T) {
	f := NewFilter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.AllowIP("10.0.0.0/8")
			f.DenyIP("192.168.0.0/16")
			f.IsAllowed(net.ParseIP("10.0.0.1"))
		}()
	}
	wg.Wait()
}

func TestAddTrustedProxy(t *testing.T) {
	f := NewFilter()
	if err := f.AddTrustedProxy("not-cidr"); err == nil {
		t.Error("expected error for invalid CIDR")
	}
}
