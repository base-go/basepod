package caddy

import (
	"encoding/json"
	"strings"
	"testing"
)

// Without a Cloudflare token, TLS config is on-demand only (unchanged behavior)
// — no ACME DNS policy.
func TestBuildTLSConfigNoToken(t *testing.T) {
	cfg := buildTLSConfig(3000, "")
	automation := cfg["automation"].(map[string]interface{})
	if _, ok := automation["policies"]; ok {
		t.Fatal("no token should mean no ACME policy")
	}
	ask := automation["on_demand"].(map[string]interface{})["ask"].(string)
	if !strings.Contains(ask, "/api/caddy/check") {
		t.Fatalf("on-demand ask = %q", ask)
	}
}

// With a token, TLS config gains a catch-all ACME issuer solving via the
// Cloudflare DNS-01 challenge — the fix for issuance behind Cloudflare's proxy.
func TestBuildTLSConfigWithCloudflareDNS(t *testing.T) {
	cfg := buildTLSConfig(3000, "cf-secret-token")
	blob, _ := json.Marshal(cfg)
	s := string(blob)

	automation := cfg["automation"].(map[string]interface{})
	policies, ok := automation["policies"].([]interface{})
	if !ok || len(policies) != 1 {
		t.Fatalf("expected one policy, got %v", automation["policies"])
	}
	issuer := policies[0].(map[string]interface{})["issuers"].([]interface{})[0].(map[string]interface{})
	if issuer["module"] != "acme" {
		t.Errorf("issuer module = %v, want acme", issuer["module"])
	}
	provider := issuer["challenges"].(map[string]interface{})["dns"].(map[string]interface{})["provider"].(map[string]interface{})
	if provider["name"] != "cloudflare" || provider["api_token"] != "cf-secret-token" {
		t.Errorf("dns provider = %v", provider)
	}
	// on-demand must still be present alongside the DNS issuer.
	if !strings.Contains(s, "/api/caddy/check") {
		t.Error("on-demand ask dropped when token set")
	}
}
