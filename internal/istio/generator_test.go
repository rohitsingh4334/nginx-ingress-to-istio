package istio

import (
	"strings"
	"testing"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
)

var defaultCfg = DefaultConfig()

func result(name, ns string, hosts []string, tlsEnabled bool, rules []analyzer.RuleResult, anns []analyzer.AnnotationNote) analyzer.IngressResult {
	return analyzer.IngressResult{
		Name:        name,
		Namespace:   ns,
		Hosts:       hosts,
		TLSEnabled:  tlsEnabled,
		Rules:       rules,
		Annotations: anns,
	}
}

func rule(host string, paths ...analyzer.PathResult) analyzer.RuleResult {
	return analyzer.RuleResult{Host: host, Paths: paths}
}

func path(p, pt, svc, port string) analyzer.PathResult {
	return analyzer.PathResult{Path: p, PathType: pt, BackendSvc: svc, BackendPort: port}
}

// ── Gateway ──────────────────────────────────────────────────────────────────

func TestBuildGateway_HTTPOnly(t *testing.T) {
	r := result("my-ing", "default", []string{"app.example.com"}, false,
		[]analyzer.RuleResult{rule("app.example.com", path("/", "Prefix", "svc", "80"))}, nil)
	got := buildGateway(r, defaultCfg)
	if !strings.Contains(got, "number: 80") {
		t.Error("expected port 80 in HTTP-only gateway")
	}
	if strings.Contains(got, "number: 443") {
		t.Error("expected no port 443 in HTTP-only gateway")
	}
	if strings.Contains(got, "tls:") {
		t.Error("expected no TLS block in HTTP-only gateway")
	}
}

func TestBuildGateway_TLSDualBlocks(t *testing.T) {
	r := result("my-ing", "default", []string{"app.example.com"}, true,
		[]analyzer.RuleResult{rule("app.example.com", path("/", "Prefix", "svc", "443"))}, nil)
	got := buildGateway(r, defaultCfg)
	if !strings.Contains(got, "number: 80") {
		t.Error("expected port 80 HTTP block in TLS gateway")
	}
	if !strings.Contains(got, "number: 443") {
		t.Error("expected port 443 HTTPS block in TLS gateway")
	}
	if !strings.Contains(got, "tls:") {
		t.Error("expected TLS block in TLS gateway")
	}
	if !strings.Contains(got, "mode: SIMPLE") {
		t.Errorf("expected TLS mode SIMPLE, got:\n%s", got)
	}
}

func TestBuildGateway_TLSModeCustom(t *testing.T) {
	cfg := Config{ConnectTimeout: "10s", LoadBalancer: "ROUND_ROBIN", TLSMode: "MUTUAL"}
	r := result("my-ing", "default", []string{"app.example.com"}, true,
		[]analyzer.RuleResult{rule("app.example.com", path("/", "Prefix", "svc", "443"))}, nil)
	got := buildGateway(r, cfg)
	if !strings.Contains(got, "mode: MUTUAL") {
		t.Errorf("expected TLS mode MUTUAL, got:\n%s", got)
	}
}

// ── VirtualService ────────────────────────────────────────────────────────────

func TestBuildVirtualService_SingleHost(t *testing.T) {
	r := result("my-ing", "default", []string{"app.example.com"}, false,
		[]analyzer.RuleResult{rule("app.example.com", path("/api", "Prefix", "api-svc", "8080"))}, nil)
	got := buildVirtualService(r)
	if !strings.Contains(got, "prefix: \"/api\"") {
		t.Errorf("expected uri prefix match, got:\n%s", got)
	}
	if !strings.Contains(got, "host: api-svc") {
		t.Errorf("expected backend host, got:\n%s", got)
	}
	if !strings.Contains(got, "number: 8080") {
		t.Errorf("expected numeric port, got:\n%s", got)
	}
}

func TestBuildVirtualService_MultiHostAuthorityMatch(t *testing.T) {
	r := result("multi", "default", []string{"a.example.com", "b.example.com"}, false,
		[]analyzer.RuleResult{
			rule("a.example.com", path("/", "Prefix", "svc-a", "80")),
			rule("b.example.com", path("/", "Prefix", "svc-b", "80")),
		}, nil)
	got := buildVirtualService(r)
	if !strings.Contains(got, `authority:`) {
		t.Errorf("expected authority match for multi-host ingress, got:\n%s", got)
	}
	if !strings.Contains(got, `exact: "a.example.com"`) {
		t.Errorf("expected authority match for a.example.com, got:\n%s", got)
	}
	if !strings.Contains(got, `exact: "b.example.com"`) {
		t.Errorf("expected authority match for b.example.com, got:\n%s", got)
	}
}

func TestBuildVirtualService_NamedPort(t *testing.T) {
	r := result("my-ing", "default", []string{"app.example.com"}, false,
		[]analyzer.RuleResult{rule("app.example.com", path("/", "Prefix", "svc", "http"))}, nil)
	got := buildVirtualService(r)
	if !strings.Contains(got, "name: http") {
		t.Errorf("expected named port 'name: http', got:\n%s", got)
	}
	if strings.Contains(got, "number: http") {
		t.Errorf("port name should not use 'number:' key, got:\n%s", got)
	}
}

// ── DestinationRule ───────────────────────────────────────────────────────────

func TestBuildDestinationRule_NumericPort(t *testing.T) {
	r := result("my-ing", "default", []string{"app.example.com"}, false,
		[]analyzer.RuleResult{rule("app.example.com", path("/", "Prefix", "my-svc", "8080"))}, nil)
	got := buildDestinationRule(r, defaultCfg)
	if !strings.Contains(got, "host: my-svc") {
		t.Errorf("expected host field, got:\n%s", got)
	}
	if !strings.Contains(got, "connectTimeout: 10s") {
		t.Errorf("expected connectTimeout, got:\n%s", got)
	}
	if !strings.Contains(got, "simple: ROUND_ROBIN") {
		t.Errorf("expected ROUND_ROBIN, got:\n%s", got)
	}
}

func TestBuildDestinationRule_NoBackends(t *testing.T) {
	r := result("my-ing", "default", []string{"app.example.com"}, false, nil, nil)
	got := buildDestinationRule(r, defaultCfg)
	if !strings.Contains(got, "No backend services detected") {
		t.Errorf("expected no-backend comment, got:\n%s", got)
	}
}

func TestBuildDestinationRule_CustomConfig(t *testing.T) {
	cfg := Config{ConnectTimeout: "5s", LoadBalancer: "LEAST_CONN", TLSMode: "SIMPLE"}
	r := result("my-ing", "default", []string{"app.example.com"}, false,
		[]analyzer.RuleResult{rule("app.example.com", path("/", "Prefix", "svc", "80"))}, nil)
	got := buildDestinationRule(r, cfg)
	if !strings.Contains(got, "connectTimeout: 5s") {
		t.Errorf("expected custom connectTimeout, got:\n%s", got)
	}
	if !strings.Contains(got, "simple: LEAST_CONN") {
		t.Errorf("expected LEAST_CONN, got:\n%s", got)
	}
}

// ── CORS Policy ───────────────────────────────────────────────────────────────

func TestBuildCORSPolicy_Disabled(t *testing.T) {
	got := buildCORSPolicy(nil)
	if got != "" {
		t.Errorf("expected empty CORS policy when disabled, got %q", got)
	}
}

func TestBuildCORSPolicy_WildcardOrigin(t *testing.T) {
	notes := []analyzer.AnnotationNote{
		{Annotation: "nginx.ingress.kubernetes.io/enable-cors", Value: "true"},
	}
	got := buildCORSPolicy(notes)
	if !strings.Contains(got, `regex: ".*"`) {
		t.Errorf("wildcard origin should emit regex, got:\n%s", got)
	}
	if strings.Contains(got, `exact: "*"`) {
		t.Errorf("wildcard origin must not emit exact:\"*\" (invalid Istio), got:\n%s", got)
	}
}

func TestBuildCORSPolicy_SpecificOrigin(t *testing.T) {
	notes := []analyzer.AnnotationNote{
		{Annotation: "nginx.ingress.kubernetes.io/enable-cors", Value: "true"},
		{Annotation: "nginx.ingress.kubernetes.io/cors-allow-origin", Value: "https://frontend.example.com"},
	}
	got := buildCORSPolicy(notes)
	if !strings.Contains(got, `exact: "https://frontend.example.com"`) {
		t.Errorf("specific origin should emit exact match, got:\n%s", got)
	}
}

// ── portField ─────────────────────────────────────────────────────────────────

func TestPortField(t *testing.T) {
	tests := []struct {
		port string
		want string
	}{
		{"8080", "number: 8080"},
		{"80", "number: 80"},
		{"http", "name: http"},
		{"grpc", "name: grpc"},
		{"", "name: "},
	}
	for _, tc := range tests {
		if got := portField(tc.port); got != tc.want {
			t.Errorf("portField(%q) = %q, want %q", tc.port, got, tc.want)
		}
	}
}
