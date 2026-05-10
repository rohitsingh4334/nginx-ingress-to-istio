package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/istio"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/k8s"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/report"
)

var version = "dev"

var validLoadBalancers = map[string]bool{
	"ROUND_ROBIN": true,
	"LEAST_CONN":  true,
	"RANDOM":      true,
	"PASSTHROUGH": true,
}

var validTLSModes = map[string]bool{
	"SIMPLE":      true,
	"MUTUAL":      true,
	"PASSTHROUGH": true,
	"AUTO_PASSTHROUGH": true,
	"ISTIO_MUTUAL": true,
}

func main() {
	app := &cli.App{
		Name:  "ingress-nginx-migration",
		Usage: "Analyze NGINX Ingresses to build a migration report to Istio",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "addr",
				Value:   ":8080",
				Usage:   "Defines the address to listen on for serving the migration report.",
				EnvVars: []string{"ADDR"},
			},
			&cli.StringFlag{
				Name:    "kubeconfig",
				Usage:   "Defines the kubeconfig file to use to connect to the Kubernetes cluster.",
				EnvVars: []string{"KUBECONFIG"},
			},
			&cli.StringSliceFlag{
				Name:    "namespaces",
				Usage:   "Defines the namespaces to analyze. When empty, all namespaces are analyzed.",
				EnvVars: []string{"NAMESPACES"},
			},
			&cli.StringFlag{
				Name:    "ingress-class",
				Usage:   "Defines the name of the ingress class this controller satisfies. Auto-detected from IngressClass resources if not set.",
				EnvVars: []string{"INGRESS_CLASS"},
			},
			&cli.StringFlag{
				Name:    "controller-class",
				Value:   "k8s.io/ingress-nginx",
				Usage:   "Defines the Ingress Controller class to analyze.",
				EnvVars: []string{"CONTROLLER_CLASS"},
			},
			&cli.BoolFlag{
				Name:    "watch-ingress-without-class",
				Usage:   "Also watch for Ingresses without an IngressClass or annotation.",
				EnvVars: []string{"WATCH_INGRESS_WITHOUT_CLASS"},
			},
			&cli.BoolFlag{
				Name:    "ingress-class-by-name",
				Usage:   "Watch for Ingress Class by Name together with Controller Class.",
				EnvVars: []string{"INGRESS_CLASS_BY_NAME"},
			},
			&cli.StringFlag{
				Name:    "connect-timeout",
				Value:   "10s",
				Usage:   "TCP connect timeout for generated DestinationRule trafficPolicy (e.g. 10s, 500ms).",
				EnvVars: []string{"CONNECT_TIMEOUT"},
			},
			&cli.StringFlag{
				Name:    "load-balancer",
				Value:   "ROUND_ROBIN",
				Usage:   "Load balancer algorithm for generated DestinationRule (ROUND_ROBIN, LEAST_CONN, RANDOM, PASSTHROUGH).",
				EnvVars: []string{"LOAD_BALANCER"},
			},
			&cli.StringFlag{
				Name:    "tls-mode",
				Value:   "SIMPLE",
				Usage:   "TLS mode for generated Gateway TLS block (SIMPLE, MUTUAL, PASSTHROUGH, AUTO_PASSTHROUGH, ISTIO_MUTUAL).",
				EnvVars: []string{"TLS_MODE"},
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "version",
				Usage: "Shows the current version",
				Action: func(c *cli.Context) error {
					log.Printf("ingress-nginx-migration version: %s\n", version)
					return nil
				},
			},
		},
		Action: func(c *cli.Context) error {
			// Validate --connect-timeout
			if _, err := time.ParseDuration(c.String("connect-timeout")); err != nil {
				return fmt.Errorf("invalid --connect-timeout %q: must be a valid duration (e.g. 10s, 500ms)", c.String("connect-timeout"))
			}
			// Validate --load-balancer
			if !validLoadBalancers[c.String("load-balancer")] {
				return fmt.Errorf("invalid --load-balancer %q: must be one of ROUND_ROBIN, LEAST_CONN, RANDOM, PASSTHROUGH", c.String("load-balancer"))
			}
			// Validate --tls-mode
			if !validTLSModes[c.String("tls-mode")] {
				return fmt.Errorf("invalid --tls-mode %q: must be one of SIMPLE, MUTUAL, PASSTHROUGH, AUTO_PASSTHROUGH, ISTIO_MUTUAL", c.String("tls-mode"))
			}

			cfg := k8s.Config{
				Kubeconfig:               c.String("kubeconfig"),
				Namespaces:               c.StringSlice("namespaces"),
				IngressClass:             c.String("ingress-class"),
				ControllerClass:          c.String("controller-class"),
				WatchIngressWithoutClass: c.Bool("watch-ingress-without-class"),
				IngressClassByName:       c.Bool("ingress-class-by-name"),
			}
			istioCfg := istio.Config{
				ConnectTimeout: c.String("connect-timeout"),
				LoadBalancer:   c.String("load-balancer"),
				TLSMode:        c.String("tls-mode"),
			}

			srv := report.NewServer(c.String("addr"), cfg, istioCfg)
			log.Printf("Serving migration report at http://%s\n", c.String("addr"))
			return srv.Serve()
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
