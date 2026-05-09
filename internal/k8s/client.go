package k8s

import (
	"context"
	"os"
	"path/filepath"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	annotationIngressClass = "kubernetes.io/ingress.class"
	defaultControllerClass = "k8s.io/ingress-nginx"
)

type Config struct {
	Kubeconfig               string
	Namespaces               []string
	IngressClass             string
	ControllerClass          string
	WatchIngressWithoutClass bool
	IngressClassByName       bool
}

type Client struct {
	cs  kubernetes.Interface
	cfg Config
}

func NewClient(cfg Config) (*Client, error) {
	rc, err := buildRestConfig(cfg.Kubeconfig)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, err
	}
	return &Client{cs: cs, cfg: cfg}, nil
}

func (c *Client) ListIngresses(ctx context.Context) ([]networkingv1.Ingress, error) {
	namespaces := c.cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}
	var all []networkingv1.Ingress
	for _, ns := range namespaces {
		list, err := c.cs.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, ing := range list.Items {
			if c.matches(ing) {
				all = append(all, ing)
			}
		}
	}
	return all, nil
}

func (c *Client) matches(ing networkingv1.Ingress) bool {
	ctrl := c.cfg.ControllerClass
	if ctrl == "" {
		ctrl = defaultControllerClass
	}
	hasClass := ing.Spec.IngressClassName != nil || ing.Annotations[annotationIngressClass] != ""
	if !hasClass {
		return c.cfg.WatchIngressWithoutClass
	}
	if ann := ing.Annotations[annotationIngressClass]; ann != "" {
		if ann == c.cfg.IngressClass || ann == ctrl {
			return true
		}
	}
	if ing.Spec.IngressClassName != nil {
		name := *ing.Spec.IngressClassName
		if name == c.cfg.IngressClass {
			return true
		}
		if c.cfg.IngressClassByName && name == ctrl {
			return true
		}
	}
	return false
}

func buildRestConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".kube", "config")
			if _, err := os.Stat(candidate); err == nil {
				kubeconfig = candidate
			}
		}
	}
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
