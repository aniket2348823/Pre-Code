package ipfilter

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
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

func TestAllowIP_IPv6Single(t *testing.T) {
	f := NewFilter()
	if err := f.AllowIP("2001:db8::1"); err != nil {
		t.Fatalf("AllowIP IPv6 single: %v", err)
	}
	if !f.IsAllowed(net.ParseIP("2001:db8::1")) {
		t.Fatal("expected exact IPv6 to be allowed")
	}
	if f.IsAllowed(net.ParseIP("2001:db8::2")) {
		t.Fatal("expected different IPv6 to be denied")
	}
}

func TestDenyIP_SingleIPv4(t *testing.T) {
	f := NewFilter()
	if err := f.DenyIP("10.1.2.3"); err != nil {
		t.Fatalf("DenyIP single IPv4: %v", err)
	}
	if f.IsAllowed(net.ParseIP("10.1.2.3")) {
		t.Fatal("expected exact IPv4 to be denied")
	}
	if !f.IsAllowed(net.ParseIP("10.1.2.4")) {
		t.Fatal("expected different IPv4 to be allowed")
	}
}

func TestDenyIP_SingleIPv6(t *testing.T) {
	f := NewFilter()
	if err := f.DenyIP("2001:db8::2"); err != nil {
		t.Fatalf("DenyIP single IPv6: %v", err)
	}
	if f.IsAllowed(net.ParseIP("2001:db8::2")) {
		t.Fatal("expected exact IPv6 to be denied")
	}
	if !f.IsAllowed(net.ParseIP("2001:db8::3")) {
		t.Fatal("expected different IPv6 to be allowed")
	}
}

func TestDenyIP_InvalidIP(t *testing.T) {
	f := NewFilter()
	if err := f.DenyIP("not-an-ip"); err == nil {
		t.Fatal("expected error for invalid IP")
	}
}

func TestAddTrustedProxy_SingleIPv4(t *testing.T) {
	f := NewFilter()
	if err := f.AddTrustedProxy("10.1.2.3"); err != nil {
		t.Fatalf("AddTrustedProxy single IPv4: %v", err)
	}
	s := f.Summary()
	if s["proxy_count"] != 1 {
		t.Fatalf("expected 1 proxy, got %v", s["proxy_count"])
	}
}

func TestAddTrustedProxy_SingleIPv6(t *testing.T) {
	f := NewFilter()
	if err := f.AddTrustedProxy("2001:db8::1"); err != nil {
		t.Fatalf("AddTrustedProxy single IPv6: %v", err)
	}
	s := f.Summary()
	if s["proxy_count"] != 1 {
		t.Fatalf("expected 1 proxy, got %v", s["proxy_count"])
	}
}

func TestAddTrustedProxy_Invalid(t *testing.T) {
	f := NewFilter()
	if err := f.AddTrustedProxy("not-cidr"); err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestMiddleware_NilClientIP(t *testing.T) {
	f := NewFilter()
	f.DenyAll()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := f.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "malformed"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestExtractClientIP_XRealIPTrusted(t *testing.T) {
	f := NewFilter()
	f.AddTrustedProxy("10.0.0.0/8")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Real-Ip", "10.0.0.5")
	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "10.0.0.1" {
		t.Fatalf("expected remote addr 10.0.0.1 for trusted X-Real-Ip, got %v", ip)
	}
}

func TestExtractClientIP_XFFAllTrusted(t *testing.T) {
	f := NewFilter()
	f.AddTrustedProxy("0.0.0.0/0")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:8080"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	ip := f.ExtractClientIP(req)
	if ip == nil || ip.String() != "192.168.1.1" {
		t.Fatalf("expected remote addr when all proxies trusted, got %v", ip)
	}
}

func TestSummary_AfterDenyAll(t *testing.T) {
	f := NewFilter()
	f.DenyAll()
	s := f.Summary()
	if !s["deny_all"].(bool) {
		t.Error("expected deny_all=true")
	}
	if !s["allow_all"].(bool) {
		t.Error("expected allow_all=true still")
	}
}

func TestConcurrentExtractAndCheck(t *testing.T) {
	f := NewFilter()
	f.AllowIP("10.0.0.0/8")
	f.DenyIP("192.168.0.0/16")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "10.0.0.1:1234"
			f.ExtractClientIP(req)
			f.IsAllowed(net.ParseIP("10.0.0.1"))
			f.Summary()
		}()
	}
	wg.Wait()
}
