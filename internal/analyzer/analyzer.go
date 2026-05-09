package analyzer

import (
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
)

type Complexity string

const (
	Low    Complexity = "Low"
	Medium Complexity = "Medium"
	High   Complexity = "High"
)

type AnnotationNote struct {
	Annotation   string
	Value        string
	IstioEquiv   string
	ManualAction string
}

type IngressResult struct {
	Name        string
	Namespace   string
	Complexity  Complexity
	Hosts       []string
	TLSEnabled  bool
	TLSSecrets  []string
	Rules       []RuleResult
	Annotations []AnnotationNote
	Warnings    []string
}

type RuleResult struct {
	Host  string
	Paths []PathResult
}

type PathResult struct {
	Path        string
	PathType    string
	BackendSvc  string
	BackendPort string
}

func Analyze(ingresses []networkingv1.Ingress) []IngressResult {
	results := make([]IngressResult, 0, len(ingresses))
	for _, ing := range ingresses {
		results = append(results, analyzeOne(ing))
	}
	return results
}

func analyzeOne(ing networkingv1.Ingress) IngressResult {
	r := IngressResult{Name: ing.Name, Namespace: ing.Namespace}
	for _, tls := range ing.Spec.TLS {
		r.TLSEnabled = true
		r.TLSSecrets = append(r.TLSSecrets, tls.SecretName)
		r.Hosts = append(r.Hosts, tls.Hosts...)
	}
	for _, rule := range ing.Spec.Rules {
		rr := RuleResult{Host: rule.Host}
		if !contains(r.Hosts, rule.Host) {
			r.Hosts = append(r.Hosts, rule.Host)
		}
		if rule.HTTP != nil {
			for _, p := range rule.HTTP.Paths {
				pt := ""
				if p.PathType != nil {
					pt = string(*p.PathType)
				}
				port := ""
				if p.Backend.Service != nil {
					if p.Backend.Service.Port.Name != "" {
						port = p.Backend.Service.Port.Name
					} else {
						port = fmt.Sprintf("%d", p.Backend.Service.Port.Number)
					}
				}
				svc := ""
				if p.Backend.Service != nil {
					svc = p.Backend.Service.Name
				}
				rr.Paths = append(rr.Paths, PathResult{Path: p.Path, PathType: pt, BackendSvc: svc, BackendPort: port})
			}
		}
		r.Rules = append(r.Rules, rr)
	}
	r.Annotations, r.Warnings = parseAnnotations(ing.Annotations)
	r.Complexity = scoreComplexity(r.Annotations)
	return r
}

var annotationMap = []struct {
	prefix       string
	istioEquiv   string
	manualAction string
	weight       int
}{
	{"nginx.ingress.kubernetes.io/rewrite-target", "HTTPRewrite in VirtualService", "Define rewrite rules in HTTPRoute.rewrite", 2},
	{"nginx.ingress.kubernetes.io/ssl-redirect", "TLS mode in Gateway", "Set tls.mode=SIMPLE on Gateway listener", 1},
	{"nginx.ingress.kubernetes.io/force-ssl-redirect", "TLS mode in Gateway", "Enforce HTTPS via Gateway tls.httpsRedirect", 1},
	{"nginx.ingress.kubernetes.io/backend-protocol", "DestinationRule trafficPolicy", "Set TLS or gRPC settings in DestinationRule", 2},
	{"nginx.ingress.kubernetes.io/ssl-passthrough", "Gateway TLS passthrough", "Use tls.mode=PASSTHROUGH in Gateway", 3},
	{"nginx.ingress.kubernetes.io/auth-type", "No direct equivalent", "Use Istio AuthorizationPolicy or external auth via EnvoyFilter", 3},
	{"nginx.ingress.kubernetes.io/auth-url", "No direct equivalent", "Implement via Istio ext_authz EnvoyFilter", 3},
	{"nginx.ingress.kubernetes.io/auth-secret", "No direct equivalent", "Replace with RequestAuthentication + AuthorizationPolicy", 3},
	{"nginx.ingress.kubernetes.io/limit-rps", "No direct equivalent", "Use EnvoyFilter local_ratelimit or an external rate-limit service", 3},
	{"nginx.ingress.kubernetes.io/limit-connections", "No direct equivalent", "Use EnvoyFilter for connection limits", 3},
	{"nginx.ingress.kubernetes.io/enable-cors", "CorsPolicy in VirtualService", "Define corsPolicy block in VirtualService HTTPRoute", 2},
	{"nginx.ingress.kubernetes.io/cors-allow-origin", "CorsPolicy.allowOrigins", "Set corsPolicy.allowOrigins in VirtualService", 2},
	{"nginx.ingress.kubernetes.io/cors-allow-methods", "CorsPolicy.allowMethods", "Set corsPolicy.allowMethods in VirtualService", 1},
	{"nginx.ingress.kubernetes.io/cors-allow-headers", "CorsPolicy.allowHeaders", "Set corsPolicy.allowHeaders in VirtualService", 1},
	{"nginx.ingress.kubernetes.io/proxy-body-size", "No direct equivalent", "Configure via EnvoyFilter or mesh config", 2},
	{"nginx.ingress.kubernetes.io/proxy-read-timeout", "HTTPRoute timeout in VirtualService", "Set route.timeout in VirtualService HTTPRoute", 1},
	{"nginx.ingress.kubernetes.io/proxy-connect-timeout", "DestinationRule connectionPool", "Set connectionPool.tcp.connectTimeout in DestinationRule", 1},
	{"nginx.ingress.kubernetes.io/proxy-send-timeout", "HTTPRoute timeout in VirtualService", "Set route.timeout in VirtualService HTTPRoute", 1},
	{"nginx.ingress.kubernetes.io/upstream-hash-by", "DestinationRule loadBalancer", "Use consistentHash in DestinationRule loadBalancer", 2},
	{"nginx.ingress.kubernetes.io/load-balance", "DestinationRule loadBalancer", "Set loadBalancer.simple in DestinationRule", 1},
	{"nginx.ingress.kubernetes.io/canary", "VirtualService weight-based routing", "Split traffic via HTTPRoute.route[].weight", 2},
	{"nginx.ingress.kubernetes.io/canary-weight", "VirtualService weight-based routing", "Set weight on each HTTPRouteDestination", 2},
	{"nginx.ingress.kubernetes.io/canary-by-header", "VirtualService header match", "Use HTTPMatchRequest.headers in VirtualService", 2},
	{"nginx.ingress.kubernetes.io/configuration-snippet", "EnvoyFilter", "Migrate Lua/NGINX snippets to EnvoyFilter patches", 3},
	{"nginx.ingress.kubernetes.io/server-snippet", "EnvoyFilter", "Migrate server-level snippets to EnvoyFilter", 3},
	{"nginx.ingress.kubernetes.io/use-regex", "VirtualService regex match", "Use StringMatch.regex in HTTPMatchRequest", 1},
	{"nginx.ingress.kubernetes.io/app-root", "HTTPRewrite in VirtualService", "Redirect root via HTTPRewrite or HTTPDirectResponse", 2},
	{"nginx.ingress.kubernetes.io/from-to-www-redirect", "VirtualService redirect", "Use HTTPRedirect in VirtualService for www redirect", 1},
	{"nginx.ingress.kubernetes.io/whitelist-source-range", "AuthorizationPolicy ipBlocks", "Use AuthorizationPolicy with ipBlocks source", 2},
	{"nginx.ingress.kubernetes.io/satisfy", "AuthorizationPolicy", "Combine AuthorizationPolicy rules for AND/OR logic", 2},
	{"nginx.ingress.kubernetes.io/permanent-redirect", "HTTPRedirect in VirtualService", "Use redirectCode=301 in VirtualService HTTPRedirect", 1},
	{"nginx.ingress.kubernetes.io/temporal-redirect", "HTTPRedirect in VirtualService", "Use redirectCode=302 in VirtualService HTTPRedirect", 1},
}

func parseAnnotations(anns map[string]string) ([]AnnotationNote, []string) {
	var notes []AnnotationNote
	var warnings []string
	for _, rule := range annotationMap {
		if val, ok := anns[rule.prefix]; ok {
			notes = append(notes, AnnotationNote{
				Annotation:   rule.prefix,
				Value:        val,
				IstioEquiv:   rule.istioEquiv,
				ManualAction: rule.manualAction,
			})
			if strings.Contains(rule.istioEquiv, "No direct") {
				warnings = append(warnings, fmt.Sprintf("No direct Istio equivalent for %q — manual migration required.", rule.prefix))
			}
		}
	}
	for k := range anns {
		if strings.HasPrefix(k, "nginx.ingress.kubernetes.io/") {
			found := false
			for _, rule := range annotationMap {
				if rule.prefix == k {
					found = true
					break
				}
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("Unknown annotation %q — review manually.", k))
				notes = append(notes, AnnotationNote{
					Annotation:   k,
					Value:        anns[k],
					IstioEquiv:   "Unknown",
					ManualAction: "Review this annotation manually — no mapping found.",
				})
			}
		}
	}
	return notes, warnings
}

func scoreComplexity(notes []AnnotationNote) Complexity {
	score := 0
	for _, rule := range annotationMap {
		for _, n := range notes {
			if n.Annotation == rule.prefix {
				score += rule.weight
			}
		}
	}
	for _, n := range notes {
		if n.IstioEquiv == "Unknown" {
			score += 3
		}
	}
	switch {
	case score == 0:
		return Low
	case score <= 4:
		return Medium
	default:
		return High
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
