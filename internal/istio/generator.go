package istio

import (
	"fmt"
	"strings"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
)

type Resources struct {
	Gateway         string
	VirtualService  string
	DestinationRule string
}

func Generate(r analyzer.IngressResult) Resources {
	return Resources{
		Gateway:         buildGateway(r),
		VirtualService:  buildVirtualService(r),
		DestinationRule: buildDestinationRule(r),
	}
}

func buildGateway(r analyzer.IngressResult) string {
	hosts := uniqueHosts(r)
	tlsBlock := ""
	if r.TLSEnabled {
		tlsBlock = "    tls:\n      mode: SIMPLE\n      # TODO: reference your TLS secret via credentialName"
	}
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: %s-gateway
  namespace: %s
spec:
  selector:
    istio: ingressgateway
  servers:
    - port:
        number: %d
        name: %s
        protocol: %s
      hosts:
%s
%s`,
		r.Name, r.Namespace,
		gatewayPort(r.TLSEnabled), gatewayPortName(r.TLSEnabled), gatewayProtocol(r.TLSEnabled),
		formatHosts(hosts, "        - "), tlsBlock)
}

func buildVirtualService(r analyzer.IngressResult) string {
	hosts := uniqueHosts(r)
	var httpRoutes strings.Builder
	for _, rule := range r.Rules {
		for _, path := range rule.Paths {
			httpRoutes.WriteString(fmt.Sprintf(`    - match:
        - uri:
            %s: "%s"
      route:
        - destination:
            host: %s
            port:
              number: %s
`, pathMatchType(path.PathType), path.Path, path.BackendSvc, path.BackendPort))
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

func buildDestinationRule(r analyzer.IngressResult) string {
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
  host: %s
  trafficPolicy:
    connectionPool:
      tcp:
        connectTimeout: 10s
    loadBalancer:
      simple: ROUND_ROBIN
---
`, svc, r.Namespace, svc))
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
	return fmt.Sprintf("    corsPolicy:\n      allowOrigins:\n        - exact: \"%s\"\n      allowMethods: [%s]\n      allowHeaders: [%s]\n", allowOrigins, allowMethods, allowHeaders)
}

func uniqueHosts(r analyzer.IngressResult) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range r.Hosts {
		if h == "" { h = "*" }
		if !seen[h] { seen[h] = true; out = append(out, h) }
	}
	if len(out) == 0 { out = []string{"*"} }
	return out
}

func formatHosts(hosts []string, indent string) string {
	var sb strings.Builder
	for _, h := range hosts { sb.WriteString(indent + h + "\n") }
	return strings.TrimRight(sb.String(), "\n")
}

func gatewayPort(tls bool) int        { if tls { return 443 }; return 80 }
func gatewayPortName(tls bool) string  { if tls { return "https" }; return "http" }
func gatewayProtocol(tls bool) string  { if tls { return "HTTPS" }; return "HTTP" }
func pathMatchType(pt string) string   { if pt == "Exact" { return "exact" }; return "prefix" }
