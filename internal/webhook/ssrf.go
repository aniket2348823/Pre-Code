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
	privateRanges   []net.IPNet
	client          *http.Client
}

// NewSSRFValidator creates a validator that blocks internal/private IPs.
func NewSSRFValidator() *SSRFValidator {
	v := &SSRFValidator{
		allowedSchemes: []string{"https"},
		blockedHosts:   []string{"localhost", "127.0.0.1", "0.0.0.0", "::1", "metadata.google.internal", "169.254.169.254"},
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return fmt.Errorf("redirects not allowed for SSRF protection")
			},
		},
	}

	privateCIDRs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"0.0.0.0/8",
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

func normalizeURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.ReplaceAll(rawURL, "\t", "")
	rawURL = strings.ReplaceAll(rawURL, "\r", "")
	rawURL = strings.ReplaceAll(rawURL, "\n", "")
	rawURL = strings.ReplaceAll(rawURL, "\x00", "")
	if rawURL == "" {
		return "", fmt.Errorf("empty URL")
	}
	if strings.Contains(rawURL, "\\") {
		return "", fmt.Errorf("URL contains invalid backslash")
	}
	return rawURL, nil
}

func normalizeIP(ip net.IP) net.IP {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return ip
}

func (v *SSRFValidator) isBlockedHost(host string) error {
	lower := strings.ToLower(host)
	for _, blocked := range v.blockedHosts {
		if lower == blocked {
			return fmt.Errorf("host %q is blocked", host)
		}
	}
	if ip := net.ParseIP(lower); ip != nil {
		normalized := normalizeIP(ip)
		for _, blocked := range v.blockedHosts {
			if blockedIP := net.ParseIP(blocked); blockedIP != nil {
				if normalized.Equal(normalizeIP(blockedIP)) {
					return fmt.Errorf("IP %s is blocked", ip.String())
				}
			}
		}
		if v.isPrivateIP(ip) {
			return fmt.Errorf("IP %s is in a private/reserved range", ip.String())
		}
	}
	return nil
}

// ValidateURL checks if a URL is safe to fetch (not SSRF).
func (v *SSRFValidator) ValidateURL(rawURL string) error {
	clean, err := normalizeURL(rawURL)
	if err != nil {
		return err
	}

	parsed, err := url.Parse(clean)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

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

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	if err := v.isBlockedHost(host); err != nil {
		return err
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}

	for _, ip := range ips {
		if v.isPrivateIP(ip) {
			return fmt.Errorf("IP %s is in a private/reserved range", ip.String())
		}
		normalized := normalizeIP(ip)
		for _, blocked := range v.blockedHosts {
			if blockedIP := net.ParseIP(blocked); blockedIP != nil {
				if normalized.Equal(normalizeIP(blockedIP)) {
					return fmt.Errorf("resolved IP %s is blocked", ip.String())
				}
			}
		}
	}

	return nil
}

// isPrivateIP checks if an IP is in a private/reserved range.
func (v *SSRFValidator) isPrivateIP(ip net.IP) bool {
	ip = normalizeIP(ip)
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
