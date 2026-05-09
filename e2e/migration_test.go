package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/k8s"
)

const k3sImage = "rancher/k3s:v1.28.5-k3s1"

func TestMigrationAnnotationCompatibility(t *testing.T) {
	if os.Getenv("ISTIO_IMAGE") == "" {
		t.Skip("ISTIO_IMAGE not set — skipping e2e tests")
	}
	ctx := context.Background()
	kubeconfig := getOrCreateCluster(t, ctx)
	rc, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)
	cs, err := kubernetes.NewForConfig(rc)
	require.NoError(t, err)

	ns := "e2e-test"
	ensureNamespace(t, ctx, cs, ns)
	seedIngresses(t, ctx, cs, ns)

	cfg := k8s.Config{Kubeconfig: kubeconfig, Namespaces: []string{ns}, ControllerClass: "k8s.io/ingress-nginx", WatchIngressWithoutClass: true}
	client, err := k8s.NewClient(cfg)
	require.NoError(t, err)
	ingresses, err := client.ListIngresses(ctx)
	require.NoError(t, err)
	require.Len(t, ingresses, 3)

	results := analyzer.Analyze(ingresses)
	byName := map[string]analyzer.IngressResult{}
	for _, r := range results { byName[r.Name] = r }

	assert.Equal(t, analyzer.Low, byName["simple-ingress"].Complexity)
	assert.Equal(t, analyzer.Medium, byName["cors-ingress"].Complexity)
	assert.Equal(t, analyzer.High, byName["complex-ingress"].Complexity)
	assert.NotEmpty(t, byName["complex-ingress"].Warnings)
}

func getOrCreateCluster(t *testing.T, ctx context.Context) string {
	t.Helper()
	if os.Getenv("E2E_REUSE_CLUSTER") == "true" {
		if kc := os.Getenv("E2E_KUBECONFIG"); kc != "" {
			return kc
		}
	}
	req := testcontainers.ContainerRequest{
		Name: "e2e-k3s-cluster", Image: k3sImage,
		Cmd: []string{"server", "--disable=traefik"},
		Env: map[string]string{"K3S_KUBECONFIG_MODE": "644"},
		Privileged: true,
		WaitingFor: wait.ForLog("Node controller sync successful").WithStartupTimeout(2 * time.Minute),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req, Started: true,
		Reuse: os.Getenv("E2E_REUSE_CLUSTER") == "true",
	})
	require.NoError(t, err)
	_, reader, err := container.Exec(ctx, []string{"cat", "/etc/rancher/k3s/k3s.yaml"})
	require.NoError(t, err)
	buf := make([]byte, 8192)
	n, _ := reader.Read(buf)
	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "6443")
	kc := fmt.Sprintf("%s", buf[:n])
	_ = host; _ = port // TODO: patch server address
	f, err := os.CreateTemp("", "e2e-kubeconfig-*.yaml")
	require.NoError(t, err)
	f.WriteString(kc); f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func ensureNamespace(t *testing.T, ctx context.Context, cs kubernetes.Interface, ns string) {
	t.Helper()
	_, err := cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err == nil { return }
	ns_obj := &metav1.ObjectMeta{Name: ns}
	_ = ns_obj
}

func seedIngresses(t *testing.T, ctx context.Context, cs kubernetes.Interface, ns string) {
	t.Helper()
	prefix := networkingv1.PathTypePrefix
	ingresses := []networkingv1.Ingress{
		{ObjectMeta: metav1.ObjectMeta{Name: "simple-ingress", Namespace: ns},
			Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "simple.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &prefix,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: "simple-svc", Port: networkingv1.ServiceBackendPort{Number: 80}}}}}}}}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cors-ingress", Namespace: ns,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/enable-cors":    "true",
				"nginx.ingress.kubernetes.io/rewrite-target": "/$2"}},
			Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "cors.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{Path: "/api(/|$)(.*)", PathType: &prefix,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: "api-svc", Port: networkingv1.ServiceBackendPort{Number: 8080}}}}}}}}}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "complex-ingress", Namespace: ns,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/auth-type":             "basic",
				"nginx.ingress.kubernetes.io/limit-rps":             "10",
				"nginx.ingress.kubernetes.io/configuration-snippet": "more_set_headers 'X-Custom: true';",
				"nginx.ingress.kubernetes.io/ssl-passthrough":       "true"}},
			Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "complex.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &prefix,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: "complex-svc", Port: networkingv1.ServiceBackendPort{Number: 443}}}}}}}}}}}},
	}
	for _, ing := range ingresses {
		_, err := cs.NetworkingV1().Ingresses(ns).Create(ctx, &ing, metav1.CreateOptions{})
		if err != nil { t.Logf("Ingress %q already exists: %v", ing.Name, err) }
	}
}
