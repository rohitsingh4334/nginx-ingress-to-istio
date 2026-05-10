package istio

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
)

type Resources struct {
	Gateway         string
	VirtualService  string
	DestinationRule string
}

type Config struct {
	ConnectTimeout string // e.g. "10s"
	LoadBalancer   string // e.g. "ROUND_ROBIN"
	TLSMode        string // e.g. "SIMPLE"
}

func DefaultConfig() Config {
	return Config{
		ConnectTimeout: "10s",
		LoadBalancer:   "ROUND_ROBIN",
		TLSMode:        "SIMPLE",
	}
}

func Generate(r analyzer.IngressResult, cfg Config) Resources {
	return Resources{
		Gateway:         buildGateway(r, cfg),
		VirtualService:  buildVirtualService(r),
		DestinationRule: buildDestinationRule(r, cfg),
	}
}

func buildGateway(r analyzer.IngressResult, cfg Config) string {
	hosts := uniqueHosts(r)

	// Always include an HTTP server block.
	httpBlock := fmt.Sprintf(`  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
%s`, formatHosts(hosts, "      - "))

	if !r.TLSEnabled {
		return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: %s-gateway
  namespace: %s
spec:
  selector:
    istio: ingressgateway
  servers:
%s`, r.Name, r.Namespace, httpBlock)
	}

	// When TLS is enabled, emit both HTTP and HTTPS server blocks.
	httpsBlock := fmt.Sprintf(`  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
%s
    tls:
      mode: %s
      # TODO: reference your TLS secret via credentialName`, formatHosts(hosts, "      - "), cfg.TLSMode)

	return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: %s-gateway
  namespace: %s
spec:
  selector:
    istio: ingressgateway
  servers:
%s
%s`, r.Name, r.Namespace, httpBlock, httpsBlock)
}

func buildVirtualService(r analyzer.IngressResult) string {
	hosts := uniqueHosts(r)
	var httpRoutes strings.Builder
	for _, rule := range r.Rules {
		for _, path := range rule.Paths {
			matchBlock := fmt.Sprintf("    - match:\n        - uri:\n            %s: %q", pathMatchType(path.PathType), path.Path)
			if rule.Host != "" {
				matchBlock += fmt.Sprintf("\n          authority:\n            exact: %q", rule.Host)
			}
			matchBlock += fmt.Sprintf("\n      route:\n        - destination:\n            host: %s\n            port:\n              %s\n",
				path.BackendSvc, portField(path.BackendPort))
			httpRoutes.WriteString(matchBlock)
		}
	}
	corsBlock := buildCORSPolicy(r.Annotations)
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: %s-vs
  namespace: %s
spec:
  hosts:
%s
  gateways:
    - %s-gateway
  http:
%s%s`,
		r.Name, r.Namespace, formatHosts(hosts, "    - "), r.Name, httpRoutes.String(), corsBlock)
}

func buildDestinationRule(r analyzer.IngressResult, cfg Config) string {
	seen := map[string]bool{}
	var svcs []string
	for _, rule := range r.Rules {
		for _, p := range rule.Paths {
			if p.BackendSvc != "" && !seen[p.BackendSvc] {
				seen[p.BackendSvc] = true
				svcs = append(svcs, p.BackendSvc)
			}
		}
	}
	if len(svcs) == 0 {
		return "# No backend services detected — no DestinationRule generated."
	}
	var sb strings.Builder
	for _, svc := range svcs {
		sb.WriteString(fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: %s-dr
  namespace: %s
spec:
  # host is the service short name; use FQDN (svc.namespace.svc.cluster.local) for cross-namespace routing
  host: %s
  trafficPolicy:
    connectionPool:
      tcp:
        connectTimeout: %s
    loadBalancer:
      simple: %s
---
`, svc, r.Namespace, svc, cfg.ConnectTimeout, cfg.LoadBalancer))
	}
	return sb.String()
}

func buildCORSPolicy(notes []analyzer.AnnotationNote) string {
	corsEnabled := false
	allowOrigins, allowMethods, allowHeaders := "*", "GET, POST, PUT, DELETE, OPTIONS", "Authorization, Content-Type"
	for _, n := range notes {
		switch n.Annotation {
		case "nginx.ingress.kubernetes.io/enable-cors":
			corsEnabled = n.Value == "true"
		case "nginx.ingress.kubernetes.io/cors-allow-origin":
			allowOrigins = n.Value
		case "nginx.ingress.kubernetes.io/cors-allow-methods":
			allowMethods = n.Value
		case "nginx.ingress.kubernetes.io/cors-allow-headers":
			allowHeaders = n.Value
		}
	}
	if !corsEnabled {
		return ""
	}
	// Istio does not support exact:"*" — use regex for wildcard origins.
	originMatch := fmt.Sprintf("exact: %q", allowOrigins)
	if allowOrigins == "*" {
		originMatch = `regex: ".*"`
	}
	return fmt.Sprintf("    corsPolicy:\n      allowOrigins:\n        - %s\n      allowMethods: [%s]\n      allowHeaders: [%s]\n",
		originMatch, allowMethods, allowHeaders)
}

// portField emits the correct YAML key for a backend port:
// numeric ports use "number:", named ports use "name:".
func portField(port string) string {
	if _, err := strconv.Atoi(port); err == nil {
		return fmt.Sprintf("number: %s", port)
	}
	return fmt.Sprintf("name: %s", port)
}

func uniqueHosts(r analyzer.IngressResult) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range r.Hosts {
		if h == "" {
			h = "*"
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		out = []string{"*"}
	}
	return out
}

func formatHosts(hosts []string, indent string) string {
	var sb strings.Builder
	for _, h := range hosts {
		sb.WriteString(indent + h + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func pathMatchType(pt string) string {
	if pt == "Exact" {
		return "exact"
	}
	return "prefix"
}
