package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SSRFValidator prevents Server-Side Request Forgery in webhook URLs.
type SSRFValidator struct {
	allowedSchemes  []string
	blockedHosts    []string
	blockedRanges   []net.IPNet
	privateRanges   []net.IPNet
	client          *http.Client
}

// NewSSRFValidator creates a validator that blocks internal/private IPs.
func NewSSRFValidator() *SSRFValidator {
	v := &SSRFValidator{
		allowedSchemes: []string{"https"}, // Only HTTPS for webhooks
		blockedHosts:   []string{"localhost", "127.0.0.1", "0.0.0.0", "::1", "metadata.google.internal", "169.254.169.254"},
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return fmt.Errorf("redirects not allowed for SSRF protection")
			},
		},
	}

	// RFC 1918 + loopback + link-local
	privateCIDRs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range privateCIDRs {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil {
			v.privateRanges = append(v.privateRanges, *network)
		}
	}

	return v
}

// ValidateURL checks if a URL is safe to fetch (not SSRF).
func (v *SSRFValidator) ValidateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check scheme
	scheme := strings.ToLower(parsed.Scheme)
	allowed := false
	for _, s := range v.allowedSchemes {
		if scheme == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("scheme %q not allowed (only HTTPS)", scheme)
	}

	// Check blocked hosts
	host := strings.ToLower(parsed.Hostname())
	for _, blocked := range v.blockedHosts {
		if host == blocked {
			return fmt.Errorf("host %q is blocked", host)
		}
	}

	// Resolve and check for private IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS resolution failed — might be intentional block
		return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}

	for _, ip := range ips {
		if v.isPrivateIP(ip) {
			return fmt.Errorf("IP %s is in a private/reserved range", ip.String())
		}
	}

	return nil
}

// isPrivateIP checks if an IP is in a private/reserved range.
func (v *SSRFValidator) isPrivateIP(ip net.IP) bool {
	for _, network := range v.privateRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateEndpoint validates a webhook endpoint URL.
func (e *Engine) ValidateEndpoint(ctx context.Context, rawURL string) error {
	if e.validator == nil {
		return nil
	}
	return e.validator.ValidateURL(rawURL)
}
