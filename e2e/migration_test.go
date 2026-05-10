package e2e

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/testreport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/k8s"
)

func k3sImage() string {
	if img := os.Getenv("K3S_IMAGE"); img != "" {
		return img
	}
	return "rancher/k3s:v1.28.5-k3s1"
}

var (
	mu      sync.Mutex
	records []testreport.Result
)

func TestMain(m *testing.M) {
	start := time.Now()
	code := m.Run()

	path := os.Getenv("TEST_REPORT_PATH")
	if path == "" {
		path = "../test-report.json"
	}

	passed, failed := 0, 0
	for _, r := range records {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}

	status := testreport.StatusPassed
	if code != 0 {
		status = testreport.StatusFailed
	}

	if err := testreport.Write(path, testreport.Report{
		Status:  status,
		RunAt:   start.Format(time.RFC3339),
		Total:   len(records),
		Passed:  passed,
		Failed:  failed,
		Results: records,
	}); err != nil {
		log.Printf("WARNING: failed to write test report: %v", err)
	}

	os.Exit(code)
}

func trackResult(t *testing.T) func() {
	t.Helper()
	start := time.Now()
	return func() {
		mu.Lock()
		records = append(records, testreport.Result{
			Name:     t.Name(),
			Passed:   !t.Failed(),
			Duration: time.Since(start).Round(time.Millisecond).String(),
		})
		mu.Unlock()
	}
}

func TestMigrationAnnotationCompatibility(t *testing.T) {
	defer trackResult(t)()
	if os.Getenv("E2E_TEST") != "true" {
		t.Skip("E2E_TEST=true not set — skipping e2e tests")
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
	for _, r := range results {
		byName[r.Name] = r
	}

	assert.Equal(t, analyzer.Low, byName["simple-ingress"].Complexity)
	assert.Equal(t, analyzer.Medium, byName["cors-ingress"].Complexity)
	assert.Equal(t, analyzer.High, byName["complex-ingress"].Complexity)
	assert.NotEmpty(t, byName["complex-ingress"].Warnings)
}

func detectDockerHost(t *testing.T) {
	t.Helper()
	if os.Getenv("DOCKER_HOST") != "" {
		return
	}
	candidates := []string{
		"/var/run/docker.sock",
		os.Getenv("HOME") + "/.colima/default/docker.sock",
		"/run/user/" + fmt.Sprintf("%d", os.Getuid()) + "/docker.sock",
	}
	for _, s := range candidates {
		if _, err := os.Stat(s); err == nil {
			t.Setenv("DOCKER_HOST", "unix://"+s)
			// Ryuk mounts the socket path as a volume which Colima doesn't support.
			if s != "/var/run/docker.sock" {
				t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			}
			t.Logf("using Docker socket: %s", s)
			return
		}
	}
	t.Fatal("no Docker socket found — is Docker/Colima running?")
}

func getOrCreateCluster(t *testing.T, ctx context.Context) string {
	t.Helper()
	detectDockerHost(t)
	if os.Getenv("E2E_REUSE_CLUSTER") == "true" {
		if kc := os.Getenv("E2E_KUBECONFIG"); kc != "" {
			return kc
		}
	}
	req := testcontainers.ContainerRequest{
		Image:        k3sImage(),
		Cmd:          []string{"server", "--disable=traefik"},
		Env:          map[string]string{"K3S_KUBECONFIG_MODE": "644"},
		Privileged:   true,
		ExposedPorts: []string{"6443/tcp"},
		WaitingFor:   wait.ForLog("Node controller sync successful").WithStartupTimeout(2 * time.Minute),
	}
	reuse := os.Getenv("E2E_REUSE_CLUSTER") == "true"
	if reuse {
		req.Name = "e2e-k3s-cluster"
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Reuse:            reuse,
	})
	require.NoError(t, err)

	_, reader, err := container.Exec(ctx, []string{"cat", "/etc/rancher/k3s/k3s.yaml"})
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
	require.NoError(t, err)
	raw := stdout.Bytes()

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6443")
	require.NoError(t, err)

	// k3s writes 127.0.0.1:6443 inside the container; replace with the mapped host:port.
	kc := strings.ReplaceAll(string(raw), "127.0.0.1:6443", fmt.Sprintf("%s:%s", host, port.Port()))

	f, err := os.CreateTemp("", "e2e-kubeconfig-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(kc)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func ensureNamespace(t *testing.T, ctx context.Context, cs kubernetes.Interface, ns string) {
	t.Helper()
	_, err := cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err == nil {
		return
	}
	_, err = cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}

func seedIngresses(t *testing.T, ctx context.Context, cs kubernetes.Interface, ns string) {
	t.Helper()
	prefix := networkingv1.PathTypePrefix

	backend := func(svc string, port int32) networkingv1.IngressBackend {
		return networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: svc,
				Port: networkingv1.ServiceBackendPort{Number: port},
			},
		}
	}
	path := func(p string, b networkingv1.IngressBackend) networkingv1.HTTPIngressPath {
		return networkingv1.HTTPIngressPath{Path: p, PathType: &prefix, Backend: b}
	}
	rule := func(host string, paths ...networkingv1.HTTPIngressPath) networkingv1.IngressRule {
		return networkingv1.IngressRule{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
			},
		}
	}

	ingresses := []networkingv1.Ingress{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "simple-ingress", Namespace: ns},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					rule("simple.example.com", path("/", backend("simple-svc", 80))),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cors-ingress",
				Namespace: ns,
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/enable-cors":    "true",
					"nginx.ingress.kubernetes.io/rewrite-target": "/$2",
				},
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					rule("cors.example.com", path("/api(/|$)(.*)", backend("api-svc", 8080))),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "complex-ingress",
				Namespace: ns,
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/auth-type":             "basic",
					"nginx.ingress.kubernetes.io/limit-rps":             "10",
					"nginx.ingress.kubernetes.io/configuration-snippet": "more_set_headers 'X-Custom: true';",
					"nginx.ingress.kubernetes.io/ssl-passthrough":       "true",
				},
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					rule("complex.example.com", path("/", backend("complex-svc", 443))),
				},
			},
		},
	}

	for _, ing := range ingresses {
		_, err := cs.NetworkingV1().Ingresses(ns).Create(ctx, &ing, metav1.CreateOptions{})
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			require.NoError(t, err, "seeding ingress %q", ing.Name)
		}
	}
}
