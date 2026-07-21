package ipfilter

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewFilterAllowsAllByDefault(t *testing.T) {
	f := NewFilter()
	if !f.IsAllowed(net.ParseIP("1.2.3.4")) {
		t.Fatal("default filter should allow all IPs")
	}
}

func TestDenyIP(t *testing.T) {
	f := NewFilter()
	if err := f.DenyIP("10.0.0.0/8"); err != nil {
		t.Fatalf("DenyIP: %v", err)
	}
	if f.IsAllowed(net.ParseIP("10.1.2.3")) {
		t.Fatal("expected 10.1.2.3 to be denied")
	}
	if !f.IsAllowed(net.ParseIP("192.168.1.1")) {
		t.Fatal("expected 192.168.1.1 to be allowed")
	}
}

func TestAllowIP(t *testing.T) {
	f := NewFilter()
	if err := f.AllowIP("192.168.1.0/24"); err != nil {
		t.Fatalf("AllowIP: %v", err)
	}
	if !f.IsAllowed(net.ParseIP("192.168.1.42")) {
		t.Fatal("expected 192.168.1.42 to be allowed")
	}
	if f.IsAllowed(net.ParseIP("10.0.0.1")) {
		t.Fatal("expected 10.0.0.1 to be denied (not in allow list)")
	}
}

func TestAllowIPSingleIP(t *testing.T) {
	f := NewFilter()
	if err := f.AllowIP("192.168.1.100"); err != nil {
		t.Fatalf("AllowIP single: %v", err)
	}
	if !f.IsAllowed(net.ParseIP("192.168.1.100")) {
		t.Fatal("expected exact IP to be allowed")
	}
	if f.IsAllowed(net.ParseIP("192.168.1.101")) {
		t.Fatal("expected different IP to be denied")
	}
}

func TestDenyTakesPrecedence(t *testing.T) {
	f := NewFilter()
	_ = f.AllowIP("10.0.0.0/8")
	_ = f.DenyIP("10.0.0.1/32")
	if f.IsAllowed(net.ParseIP("10.0.0.1")) {
		t.Fatal("deny should take precedence over allow")
	}
	if !f.IsAllowed(net.ParseIP("10.0.0.2")) {
		t.Fatal("other IPs in range should still be allowed")
	}
}

func TestDenyAll(t *testing.T) {
	f := NewFilter()
	f.DenyAll()
	if f.IsAllowed(net.ParseIP("1.2.3.4")) {
		t.Fatal("DenyAll should block everything")
	}
}

func TestAllowAllClearsList(t *testing.T) {
	f := NewFilter()
	_ = f.AllowIP("192.168.1.0/24")
	f.AllowAll()
	if !f.IsAllowed(net.ParseIP("10.0.0.1")) {
		t.Fatal("AllowAll should permit everything")
	}
}

func TestMiddlewareBlocks(t *testing.T) {
	f := NewFilter()
	_ = f.DenyIP("192.168.1.0/24")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := f.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.10:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestMiddlewareAllows(t *testing.T) {
	f := NewFilter()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := f.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestExtractClientIPFromXFF(t *testing.T) {
	f := NewFilter()
	f.AddTrustedProxy("10.0.0.0/8")

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 203.0.113.5")

	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "203.0.113.5" {
		t.Fatalf("expected 203.0.113.5, got %v", ip)
	}
}

func TestExtractClientIPFromXRealIP(t *testing.T) {
	f := NewFilter()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Real-Ip", "203.0.113.10")

	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "203.0.113.10" {
		t.Fatalf("expected 203.0.113.10, got %v", ip)
	}
}

func TestSummary(t *testing.T) {
	f := NewFilter()
	_ = f.AllowIP("192.168.0.0/16")
	_ = f.DenyIP("192.168.1.0/24")
	f.AddTrustedProxy("10.0.0.0/8")

	s := f.Summary()
	if s["allow_count"] != 1 {
		t.Fatalf("expected 1 allow rule, got %v", s["allow_count"])
	}
	if s["deny_count"] != 1 {
		t.Fatalf("expected 1 deny rule, got %v", s["deny_count"])
	}
	if s["proxy_count"] != 1 {
		t.Fatalf("expected 1 proxy rule, got %v", s["proxy_count"])
	}
}

func TestInvalidCIDR(t *testing.T) {
	f := NewFilter()
	if err := f.AllowIP("not-a-cidr"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestIPv6Support(t *testing.T) {
	f := NewFilter()
	_ = f.DenyIP("::1/128")
	if f.IsAllowed(net.ParseIP("::1")) {
		t.Fatal("expected ::1 to be denied")
	}
	if !f.IsAllowed(net.ParseIP("::2")) {
		t.Fatal("expected ::2 to be allowed")
	}
}
