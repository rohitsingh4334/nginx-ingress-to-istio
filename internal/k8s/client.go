package k8s

import (
	"context"
	"fmt"
	"log"
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
	cs   kubernetes.Interface
	cfg  Config
	ctrl string // normalised controller class, set once at construction
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
	ctrl := cfg.ControllerClass
	if ctrl == "" {
		ctrl = defaultControllerClass
	}
	return &Client{cs: cs, cfg: cfg, ctrl: ctrl}, nil
}

func (c *Client) detectIngressClass(ctx context.Context) (string, error) {
	classes, err := c.cs.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, ic := range classes.Items {
		if ic.Spec.Controller == c.ctrl {
			return ic.Name, nil
		}
	}
	log.Printf("WARN no IngressClass matched controller %q — set --ingress-class explicitly if needed", c.ctrl)
	return "", nil
}

func (c *Client) ListIngresses(ctx context.Context) ([]networkingv1.Ingress, error) {
	ingressClass := c.cfg.IngressClass
	if ingressClass == "" {
		detected, err := c.detectIngressClass(ctx)
		if err != nil {
			return nil, fmt.Errorf("auto-detecting ingress class: %w", err)
		}
		if detected != "" {
			log.Printf("INFO auto-detected ingress class: %q", detected)
			ingressClass = detected
		}
	}
	namespaces := c.cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}
	var all []networkingv1.Ingress
	for _, ns := range namespaces {
		list, err := c.cs.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing ingresses in namespace %q: %w", ns, err)
		}
		for _, ing := range list.Items {
			if c.matches(ing, ingressClass) {
				all = append(all, ing)
			}
		}
	}
	return all, nil
}

func (c *Client) matches(ing networkingv1.Ingress, ingressClass string) bool {
	hasClass := ing.Spec.IngressClassName != nil || ing.Annotations[annotationIngressClass] != ""
	if !hasClass {
		return c.cfg.WatchIngressWithoutClass
	}
	if ann := ing.Annotations[annotationIngressClass]; ann != "" {
		if ann == ingressClass || ann == c.ctrl {
			return true
		}
	}
	if ing.Spec.IngressClassName != nil {
		name := *ing.Spec.IngressClassName
		if name == ingressClass {
			return true
		}
		if c.cfg.IngressClassByName && name == c.ctrl {
			return true
		}
	}
	return false
}

type ClusterInfo struct {
	Context string `json:"context"`
	Cluster string `json:"cluster"`
	Server  string `json:"server"`
}

func GetClusterInfo(kubeconfig string) (ClusterInfo, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	raw, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).RawConfig()
	if err != nil {
		return ClusterInfo{}, err
	}
	ctx := raw.CurrentContext
	info := ClusterInfo{Context: ctx}
	if c, ok := raw.Contexts[ctx]; ok {
		info.Cluster = c.Cluster
		if cl, ok := raw.Clusters[c.Cluster]; ok {
			info.Server = cl.Server
		}
	}
	return info, nil
}

func buildRestConfig(kubeconfig string) (*rest.Config, error) {
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
