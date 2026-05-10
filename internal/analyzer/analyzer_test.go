package analyzer

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ptr[T any](v T) *T { return &v }

func makeIngress(name, ns string, anns map[string]string, rules []networkingv1.IngressRule, tls []networkingv1.IngressTLS) networkingv1.Ingress {
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: anns},
		Spec:       networkingv1.IngressSpec{Rules: rules, TLS: tls},
	}
}

func makeRule(host, path string, pathType networkingv1.PathType, svc string, port int32) networkingv1.IngressRule {
	return networkingv1.IngressRule{
		Host: host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{
					Path:     path,
					PathType: &pathType,
					Backend: networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: svc,
							Port: networkingv1.ServiceBackendPort{Number: port},
						},
					},
				}},
			},
		},
	}
}

func TestAnalyzeOne_Complexity(t *testing.T) {
	tests := []struct {
		name       string
		anns       map[string]string
		wantLevel  Complexity
	}{
		{
			name:      "no annotations → Low",
			anns:      nil,
			wantLevel: Low,
		},
		{
			name: "single low-weight annotation → Medium",
			anns: map[string]string{
				"nginx.ingress.kubernetes.io/ssl-redirect": "true",
			},
			wantLevel: Medium,
		},
		{
			name: "high-weight annotation → High",
			anns: map[string]string{
				"nginx.ingress.kubernetes.io/auth-type":             "basic",
				"nginx.ingress.kubernetes.io/configuration-snippet": "some snippet",
			},
			wantLevel: High,
		},
		{
			name: "unknown annotation scores as High",
			anns: map[string]string{
				"nginx.ingress.kubernetes.io/unknown-thing": "value",
				"nginx.ingress.kubernetes.io/another-unknown": "value2",
			},
			wantLevel: High,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ing := makeIngress("test", "default", tc.anns,
				[]networkingv1.IngressRule{makeRule("example.com", "/", networkingv1.PathTypePrefix, "svc", 80)},
				nil)
			res := analyzeOne(ing)
			if res.Complexity != tc.wantLevel {
				t.Errorf("got complexity %q, want %q", res.Complexity, tc.wantLevel)
			}
		})
	}
}

func TestAnalyzeOne_TLSHostDedup(t *testing.T) {
	ing := makeIngress("tls-test", "default", nil,
		[]networkingv1.IngressRule{makeRule("app.example.com", "/", networkingv1.PathTypePrefix, "svc", 80)},
		[]networkingv1.IngressTLS{
			{Hosts: []string{"app.example.com", "app.example.com"}, SecretName: "tls-secret"},
		},
	)
	res := analyzeOne(ing)
	if !res.TLSEnabled {
		t.Fatal("expected TLSEnabled=true")
	}
	count := 0
	for _, h := range res.Hosts {
		if h == "app.example.com" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected host deduplicated to 1 occurrence, got %d in %v", count, res.Hosts)
	}
}

func TestAnalyzeOne_BackendPort(t *testing.T) {
	named := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "named-port", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				Host: "example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: ptr(networkingv1.PathTypePrefix),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "my-svc",
									Port: networkingv1.ServiceBackendPort{Name: "http"},
								},
							},
						}},
					},
				},
			}},
		},
	}
	res := analyzeOne(named)
	if len(res.Rules) == 0 || len(res.Rules[0].Paths) == 0 {
		t.Fatal("expected at least one path")
	}
	if got := res.Rules[0].Paths[0].BackendPort; got != "http" {
		t.Errorf("expected BackendPort=%q, got %q", "http", got)
	}
}

func TestParseAnnotations_Deterministic(t *testing.T) {
	anns := map[string]string{
		"nginx.ingress.kubernetes.io/auth-type":  "basic",
		"nginx.ingress.kubernetes.io/limit-rps":  "10",
		"nginx.ingress.kubernetes.io/auth-url":   "https://auth.example.com",
		"nginx.ingress.kubernetes.io/auth-secret": "mysecret",
	}
	_, w1 := parseAnnotations(anns)
	_, w2 := parseAnnotations(anns)
	if len(w1) != len(w2) {
		t.Fatalf("warning count differs between runs: %d vs %d", len(w1), len(w2))
	}
	for i := range w1 {
		if w1[i] != w2[i] {
			t.Errorf("warnings not deterministic at index %d: %q vs %q", i, w1[i], w2[i])
		}
	}
}

func TestParseAnnotations_UnknownAnnotation(t *testing.T) {
	anns := map[string]string{
		"nginx.ingress.kubernetes.io/totally-made-up": "val",
	}
	notes, warnings := parseAnnotations(anns)
	if len(notes) == 0 {
		t.Fatal("expected a note for unknown annotation")
	}
	if notes[0].IstioEquiv != "Unknown" {
		t.Errorf("expected IstioEquiv=Unknown, got %q", notes[0].IstioEquiv)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for unknown annotation")
	}
}

func TestScoreComplexity_Thresholds(t *testing.T) {
	tests := []struct {
		score int
		want  Complexity
	}{
		{0, Low},
		{1, Medium},
		{4, Medium},
		{5, High},
		{99, High},
	}
	for _, tc := range tests {
		// Build fake notes that total to the desired score using ssl-redirect (weight=1).
		var notes []AnnotationNote
		for i := 0; i < tc.score; i++ {
			notes = append(notes, AnnotationNote{
				Annotation: "nginx.ingress.kubernetes.io/ssl-redirect",
				IstioEquiv: "TLS mode in Gateway",
			})
		}
		if got := scoreComplexity(notes); got != tc.want {
			t.Errorf("score=%d: got %q, want %q", tc.score, got, tc.want)
		}
	}
}
