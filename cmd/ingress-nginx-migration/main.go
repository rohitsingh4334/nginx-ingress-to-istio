package main

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/k8s"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/report"
)

var version = "dev"

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
				Usage:   "Defines the name of the ingress class this controller satisfies.",
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
			cfg := k8s.Config{
				Kubeconfig:               c.String("kubeconfig"),
				Namespaces:               c.StringSlice("namespaces"),
				IngressClass:             c.String("ingress-class"),
				ControllerClass:          c.String("controller-class"),
				WatchIngressWithoutClass: c.Bool("watch-ingress-without-class"),
				IngressClassByName:       c.Bool("ingress-class-by-name"),
			}

			client, err := k8s.NewClient(cfg)
			if err != nil {
				return err
			}

			ingresses, err := client.ListIngresses(c.Context)
			if err != nil {
				return err
			}

			results := analyzer.Analyze(ingresses)

			srv := report.NewServer(c.String("addr"), results)
			log.Printf("Serving migration report at http://%s\n", c.String("addr"))
			return srv.Serve()
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
