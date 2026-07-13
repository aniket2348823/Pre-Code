package webhook

import (
	"net"
	"strings"
	"sync"
	"testing"
)

func newTestSSRFValidator() *SSRFValidator { return NewSSRFValidator() }

func requireDNS(t *testing.T) {
	t.Helper()
	_, err := net.LookupHost("example.com")
	if err != nil {
		t.Skip("skipping: DNS resolution unavailable")
	}
}

func TestValidateURL_Schemes(t *testing.T) {
	requireDNS(t)
	v := newTestSSRFValidator()
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"HTTPS allowed", "https://example.com", false},
		{"HTTP rejected", "http://example.com", true},
		{"FTP rejected", "ftp://example.com", true},
		{"FILE rejected", "file:///etc/passwd", true},
		{"no scheme rejected", "example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateURL_BlockedHosts(t *testing.T) {
	requireDNS(t)
	v := newTestSSRFValidator()
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"localhost", "https://localhost", true},
		{"127.0.0.1", "https://127.0.0.1", true},
		{"0.0.0.0", "https://0.0.0.0", true},
		{"::1", "https://[::1]", true},
		{"metadata.google.internal", "https://metadata.google.internal", true},
		{"169.254.169.254", "https://169.254.169.254", true},
		{"external passes", "https://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	v := newTestSSRFValidator()
	tests := []struct {
		ip       string
		expected bool
	}{
		{"10.0.0.1", true}, {"10.255.255.255", true},
		{"172.16.0.1", true}, {"172.31.255.255", true},
		{"172.15.0.1", false}, {"172.32.0.1", false},
		{"192.168.0.1", true}, {"192.169.0.1", false},
		{"127.0.0.1", true}, {"127.255.255.255", true}, {"126.255.255.255", false},
		{"169.254.0.1", true}, {"169.253.255.255", false},
		{"8.8.8.8", false}, {"1.1.1.1", false},
		{"::1", true}, {"fc00::1", true}, {"fe80::1", true},
		{"2001:db8::1", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse %q", tt.ip)
			}
			if got := v.isPrivateIP(ip); got != tt.expected {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.expected)
			}
		})
	}
}

func TestValidateURL_EmptyString(t *testing.T) {
	v := newTestSSRFValidator()
	if err := v.ValidateURL(""); err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestValidateURL_Malformed(t *testing.T) {
	v := newTestSSRFValidator()
	for _, u := range []string{"://invalid", "://missing"} {
		if err := v.ValidateURL(u); err == nil {
			t.Errorf("expected error for %q", u)
		}
	}
}

func TestValidateURL_Concurrent(t *testing.T) {
	v := newTestSSRFValidator()
	urls := []string{"https://example.com", "https://localhost", "http://10.0.0.1"}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = v.ValidateURL(urls[idx%len(urls)])
		}(i)
	}
	wg.Wait()
}

func TestNewSSRFValidator_Config(t *testing.T) {
	v := NewSSRFValidator()
	if len(v.allowedSchemes) != 1 || v.allowedSchemes[0] != "https" {
		t.Errorf("expected only HTTPS, got %v", v.allowedSchemes)
	}
	if len(v.blockedHosts) == 0 {
		t.Error("expected blocked hosts")
	}
	if len(v.privateRanges) == 0 {
		t.Error("expected private ranges")
	}
	if v.client == nil {
		t.Error("expected non-nil client")
	}
}

func TestValidateURL_PortAndFragment(t *testing.T) {
	requireDNS(t)
	v := newTestSSRFValidator()
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"port", "https://example.com:443", false},
		{"fragment", "https://example.com#section", false},
		{"query", "https://example.com?key=val", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v", tt.url, err)
			}
		})
	}
}

func TestValidateURL_ContainsScheme(t *testing.T) {
	v := newTestSSRFValidator()
	err := v.ValidateURL("http://example.com")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected scheme error, got %v", err)
	}
}

func TestValidateEndpoint_NilValidator(t *testing.T) {
	e := &Engine{validator: nil}
	err := e.ValidateEndpoint(nil, "https://example.com")
	if err != nil {
		t.Errorf("nil validator should return nil, got %v", err)
	}
}
