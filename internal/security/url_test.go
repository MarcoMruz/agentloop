package security

import (
	"strings"
	"testing"
)

func TestValidateURLInvalidURL(t *testing.T) {
	err := ValidateURL("://bad", nil, nil)
	if err == nil {
		t.Fatal("SECURITY: invalid URL should be rejected")
	}
}

func TestValidateURLRejectsNonHTTPSchemes(t *testing.T) {
	schemes := []string{"ftp://example.com", "file:///etc/passwd", "gopher://example.com", "javascript:alert(1)"}
	for _, u := range schemes {
		err := ValidateURL(u, nil, nil)
		if err == nil {
			t.Fatalf("SECURITY: non-http scheme should be rejected: %s", u)
		}
		if !strings.Contains(err.Error(), "only http/https allowed") {
			t.Fatalf("expected scheme error, got: %v", err)
		}
	}
}

func TestValidateURLBlocksLocalhostCIDR(t *testing.T) {
	blockedCIDRs := []string{"127.0.0.0/8"}
	// Use IP directly to avoid DNS lookup
	err := ValidateURL("http://127.0.0.1", blockedCIDRs, nil)
	if err == nil {
		t.Fatal("SECURITY: localhost should be blocked by 127.0.0.0/8 CIDR")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("expected SSRF error, got: %v", err)
	}
}

func TestValidateURLBlocksLocalhostLoopback(t *testing.T) {
	blockedCIDRs := []string{"127.0.0.0/8"}
	err := ValidateURL("http://127.0.0.2:8080/api", blockedCIDRs, nil)
	if err == nil {
		t.Fatal("SECURITY: 127.0.0.2 should be blocked by 127.0.0.0/8 CIDR")
	}
}

func TestValidateURLBlocksPrivateRangeCIDR(t *testing.T) {
	blockedCIDRs := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	privateIPs := []string{
		"http://10.0.0.1",
		"http://10.255.255.255",
		"http://192.168.1.1",
		"http://192.168.0.100:3000",
	}
	for _, u := range privateIPs {
		err := ValidateURL(u, blockedCIDRs, nil)
		if err == nil {
			t.Fatalf("SECURITY: private IP should be blocked: %s", u)
		}
	}
}

func TestValidateURLAllowsNonBlockedIP(t *testing.T) {
	// Only block 10.0.0.0/8, allow other ranges
	blockedCIDRs := []string{"10.0.0.0/8"}
	// 192.168.1.1 is not in 10.0.0.0/8, should pass
	err := ValidateURL("http://192.168.1.1", blockedCIDRs, nil)
	if err != nil {
		t.Fatalf("IP not in blocked CIDR should pass: %v", err)
	}
}

func TestValidateURLDomainNotAllowed(t *testing.T) {
	// Domain filtering happens before DNS lookup, so this doesn't need network
	err := ValidateURL("https://evil.com/attack", nil, []string{"github.com", "example.com"})
	if err == nil {
		t.Fatal("SECURITY: non-allowed domain should be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected domain not allowed error, got: %v", err)
	}
}

func TestValidateURLDomainSuffixNotSpoofable(t *testing.T) {
	// "notgithub.com" should NOT match allowed domain "github.com"
	// Domain filtering happens before DNS lookup
	err := ValidateURL("https://notgithub.com", nil, []string{"github.com"})
	if err == nil {
		t.Fatal("SECURITY: domain suffix spoofing should be blocked")
	}
}

func TestValidateURLEmptyDomainAllowlistPermitsAll(t *testing.T) {
	// With empty allowedDomains, domain check is skipped (but DNS still runs)
	// Use IP to avoid DNS
	err := ValidateURL("http://192.168.1.1", nil, nil)
	if err != nil {
		t.Fatalf("empty domain allowlist with no CIDR blocks should pass: %v", err)
	}
}

func TestValidateURLNoCIDRBlocksAllowsAnyIP(t *testing.T) {
	// With no blocked CIDRs, even private IPs should pass
	err := ValidateURL("http://10.0.0.1", nil, nil)
	if err != nil {
		t.Fatalf("no CIDR blocks should allow any IP: %v", err)
	}
}

func TestValidateURLSubdomainMatchesAllowedDomain(t *testing.T) {
	// "api.github.com" should match allowed domain "github.com" via suffix check
	// But this will fail on DNS, so we test the rejection case instead:
	// "api.evil.com" should not match "github.com"
	err := ValidateURL("https://api.evil.com", nil, []string{"github.com"})
	if err == nil {
		t.Fatal("SECURITY: subdomain of non-allowed domain should be rejected")
	}
}

func TestValidateURLIPWithPort(t *testing.T) {
	blockedCIDRs := []string{"127.0.0.0/8"}
	err := ValidateURL("http://127.0.0.1:8080/path", blockedCIDRs, nil)
	if err == nil {
		t.Fatal("SECURITY: localhost with port should still be blocked")
	}
}

func TestValidateURLInvalidCIDRSkipped(t *testing.T) {
	// Invalid CIDR entries should be skipped without error
	blockedCIDRs := []string{"not-a-cidr", "also-bad"}
	err := ValidateURL("http://192.168.1.1", blockedCIDRs, nil)
	if err != nil {
		t.Fatalf("invalid CIDRs should be skipped: %v", err)
	}
}
