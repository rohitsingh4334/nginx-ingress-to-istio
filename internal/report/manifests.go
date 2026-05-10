package report

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/istio"
)

// handleExportManifests streams a .tar.gz containing ready-to-apply Istio YAML
// files, one directory per ingress, plus a top-level kustomization.yaml.
//
// Layout inside the archive:
//
//	<namespace>/<name>/gateway.yaml
//	<namespace>/<name>/virtual-service.yaml
//	<namespace>/<name>/destination-rule.yaml
//	kustomization.yaml
func (s *Server) handleExportManifests(w http.ResponseWriter, r *http.Request) {
	results, err := s.fetchResults(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("istio-manifests-%s.tar.gz", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	var kustomResources []string
	now := time.Now()

	writeEntry := func(name, content string) bool {
		body := []byte(content)
		err := tw.WriteHeader(&tar.Header{
			Name:    name,
			Mode:    0644,
			Size:    int64(len(body)),
			ModTime: now,
			Typeflag: tar.TypeReg,
		})
		if err != nil {
			log.Printf("tar header error for %s: %v", name, err)
			return false
		}
		if _, err := tw.Write(body); err != nil {
			log.Printf("tar write error for %s: %v", name, err)
			return false
		}
		return true
	}

	for _, res := range results {
		ir := istio.Generate(res, s.istioCfg)
		base := res.Namespace + "/" + res.Name

		entries := []struct {
			path    string
			content string
		}{
			{base + "/gateway.yaml", ir.Gateway},
			{base + "/virtual-service.yaml", ir.VirtualService},
			{base + "/destination-rule.yaml", ir.DestinationRule},
		}

		for _, e := range entries {
			if strings.HasPrefix(strings.TrimSpace(e.content), "#") && strings.Contains(e.content, "No backend") {
				// Skip the "no backends" placeholder comment.
				continue
			}
			if writeEntry(e.path, e.content) {
				kustomResources = append(kustomResources, "- "+e.path)
			}
		}
	}

	// Top-level kustomization.yaml so users can run: kubectl apply -k .
	kustomContent := "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n" +
		strings.Join(kustomResources, "\n") + "\n"
	writeEntry("kustomization.yaml", kustomContent)

	if err := tw.Close(); err != nil {
		log.Printf("tar close error: %v", err)
	}
	if err := gw.Close(); err != nil {
		log.Printf("gzip close error: %v", err)
	}
}
