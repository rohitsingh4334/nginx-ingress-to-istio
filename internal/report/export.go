package report

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/xuri/excelize/v2"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/istio"
)

// xlsxLog logs an Excel API error if non-nil. Used for calls whose errors are
// non-recoverable at this point (headers not yet sent) but shouldn't abort the export.
func xlsxLog(err error, op string) {
	if err != nil {
		log.Printf("excel %s: %v", op, err)
	}
}

// handleExportExcel streams an Excel workbook to the client.
func (s *Server) handleExportExcel(w http.ResponseWriter, r *http.Request) {
	results, err := s.fetchResults(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f := excelize.NewFile()
	defer f.Close()

	// ── Sheet 1: Summary ────────────────────────────────────────────────
	sum := f.GetSheetName(0)
	xlsxLog(f.SetSheetName(sum, "Summary"), "SetSheetName")

	headers := []string{"Name", "Namespace", "Complexity", "Hosts", "TLS", "Warnings"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		xlsxLog(f.SetCellValue("Summary", cell, h), "SetCellValue")
	}

	styleHeader, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	if err != nil {
		log.Printf("excel header style error: %v", err)
	}
	xlsxLog(f.SetCellStyle("Summary", "A1", "F1", styleHeader), "SetCellStyle")

	complexityColors := map[analyzer.Complexity]string{
		analyzer.Low:    "C6EFCE",
		analyzer.Medium: "FFEB9C",
		analyzer.High:   "FFC7CE",
	}

	for row, res := range results {
		r := row + 2
		xlsxLog(f.SetCellValue("Summary", fmt.Sprintf("A%d", r), res.Name), "SetCellValue")
		xlsxLog(f.SetCellValue("Summary", fmt.Sprintf("B%d", r), res.Namespace), "SetCellValue")
		xlsxLog(f.SetCellValue("Summary", fmt.Sprintf("C%d", r), string(res.Complexity)), "SetCellValue")
		xlsxLog(f.SetCellValue("Summary", fmt.Sprintf("D%d", r), strings.Join(res.Hosts, ", ")), "SetCellValue")
		xlsxLog(f.SetCellValue("Summary", fmt.Sprintf("E%d", r), res.TLSEnabled), "SetCellValue")
		xlsxLog(f.SetCellValue("Summary", fmt.Sprintf("F%d", r), strings.Join(res.Warnings, "; ")), "SetCellValue")

		if color, ok := complexityColors[res.Complexity]; ok {
			style, err := f.NewStyle(&excelize.Style{
				Fill: excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1},
			})
			if err != nil {
				log.Printf("excel complexity style error: %v", err)
			} else {
				xlsxLog(f.SetCellStyle("Summary", fmt.Sprintf("C%d", r), fmt.Sprintf("C%d", r), style), "SetCellStyle")
			}
		}
	}

	xlsxLog(f.SetColWidth("Summary", "A", "F", 25), "SetColWidth")

	// ── Sheet 2: Annotations ────────────────────────────────────────────
	_, err = f.NewSheet("Annotations")
	xlsxLog(err, "NewSheet")
	annHeaders := []string{"Ingress", "Namespace", "Annotation", "Value", "Istio Equivalent", "Manual Action"}
	for i, h := range annHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		xlsxLog(f.SetCellValue("Annotations", cell, h), "SetCellValue")
	}
	xlsxLog(f.SetCellStyle("Annotations", "A1", "F1", styleHeader), "SetCellStyle")

	annRow := 2
	for _, res := range results {
		for _, ann := range res.Annotations {
			xlsxLog(f.SetCellValue("Annotations", fmt.Sprintf("A%d", annRow), res.Name), "SetCellValue")
			xlsxLog(f.SetCellValue("Annotations", fmt.Sprintf("B%d", annRow), res.Namespace), "SetCellValue")
			xlsxLog(f.SetCellValue("Annotations", fmt.Sprintf("C%d", annRow), ann.Annotation), "SetCellValue")
			xlsxLog(f.SetCellValue("Annotations", fmt.Sprintf("D%d", annRow), ann.Value), "SetCellValue")
			xlsxLog(f.SetCellValue("Annotations", fmt.Sprintf("E%d", annRow), ann.IstioEquiv), "SetCellValue")
			xlsxLog(f.SetCellValue("Annotations", fmt.Sprintf("F%d", annRow), ann.ManualAction), "SetCellValue")
			annRow++
		}
	}
	xlsxLog(f.SetColWidth("Annotations", "A", "F", 30), "SetColWidth")

	// ── Sheet 3: Istio Resources ─────────────────────────────────────────
	_, err = f.NewSheet("Istio Resources")
	xlsxLog(err, "NewSheet")
	istioHeaders := []string{"Ingress", "Namespace", "Gateway YAML", "VirtualService YAML", "DestinationRule YAML"}
	for i, h := range istioHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		xlsxLog(f.SetCellValue("Istio Resources", cell, h), "SetCellValue")
	}
	xlsxLog(f.SetCellStyle("Istio Resources", "A1", "E1", styleHeader), "SetCellStyle")

	for row, res := range results {
		ir := istio.Generate(res, s.istioCfg)
		r := row + 2
		xlsxLog(f.SetCellValue("Istio Resources", fmt.Sprintf("A%d", r), res.Name), "SetCellValue")
		xlsxLog(f.SetCellValue("Istio Resources", fmt.Sprintf("B%d", r), res.Namespace), "SetCellValue")
		xlsxLog(f.SetCellValue("Istio Resources", fmt.Sprintf("C%d", r), ir.Gateway), "SetCellValue")
		xlsxLog(f.SetCellValue("Istio Resources", fmt.Sprintf("D%d", r), ir.VirtualService), "SetCellValue")
		xlsxLog(f.SetCellValue("Istio Resources", fmt.Sprintf("E%d", r), ir.DestinationRule), "SetCellValue")
	}
	xlsxLog(f.SetColWidth("Istio Resources", "A", "E", 50), "SetColWidth")

	filename := fmt.Sprintf("ingress-migration-report-%s.xlsx", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := f.Write(w); err != nil {
		log.Printf("excel write error: %v", err)
	}
}

// handleExportPDF streams a PDF report to the client.
func (s *Server) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	results, err := s.fetchResults(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.AddPage()

	// Title
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, 10, "NGINX Ingress -> Istio Migration Report", "", 1, "C", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	pdf.CellFormat(0, 6, fmt.Sprintf("Generated: %s", time.Now().Format("2006-01-02 15:04:05")), "", 1, "C", false, 0, "")
	pdf.Ln(4)

	// Summary table
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(0, 7, "Summary")
	pdf.Ln(7)

	colW := []float64{50, 35, 30, 65, 15, 75}
	sumHeaders := []string{"Name", "Namespace", "Complexity", "Hosts", "TLS", "Warnings"}
	pdf.SetFillColor(68, 114, 196)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 8)
	for i, h := range sumHeaders {
		pdf.CellFormat(colW[i], 7, h, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 7)

	complexityRGB := map[analyzer.Complexity][3]int{
		analyzer.Low:    {198, 239, 206},
		analyzer.Medium: {255, 235, 156},
		analyzer.High:   {255, 199, 206},
	}

	for _, res := range results {
		rgb := complexityRGB[res.Complexity]
		pdf.SetFillColor(255, 255, 255)

		pdf.CellFormat(colW[0], 6, truncate(res.Name, 30), "1", 0, "", false, 0, "")
		pdf.CellFormat(colW[1], 6, truncate(res.Namespace, 20), "1", 0, "", false, 0, "")
		pdf.SetFillColor(int(rgb[0]), int(rgb[1]), int(rgb[2]))
		pdf.CellFormat(colW[2], 6, string(res.Complexity), "1", 0, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255)
		pdf.CellFormat(colW[3], 6, truncate(strings.Join(res.Hosts, ", "), 40), "1", 0, "", false, 0, "")
		tls := "No"
		if res.TLSEnabled {
			tls = "Yes"
		}
		pdf.CellFormat(colW[4], 6, tls, "1", 0, "C", false, 0, "")
		pdf.CellFormat(colW[5], 6, truncate(strings.Join(res.Warnings, "; "), 50), "1", 0, "", false, 0, "")
		pdf.Ln(-1)
	}

	pdf.Ln(6)

	// Per-ingress annotation details
	for _, res := range results {
		if len(res.Annotations) == 0 {
			continue
		}
		if pdf.GetY() > 170 {
			pdf.AddPage()
		}
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(0, 7, fmt.Sprintf("Annotations: %s / %s", res.Namespace, res.Name), "", 1, "", false, 0, "")

		annColW := []float64{90, 30, 70, 85}
		annHeaders := []string{"Annotation", "Value", "Istio Equivalent", "Manual Action"}
		pdf.SetFillColor(68, 114, 196)
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Arial", "B", 7)
		for i, h := range annHeaders {
			pdf.CellFormat(annColW[i], 6, h, "1", 0, "C", true, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Arial", "", 6)

		for _, ann := range res.Annotations {
			pdf.CellFormat(annColW[0], 5, truncate(ann.Annotation, 55), "1", 0, "", false, 0, "")
			pdf.CellFormat(annColW[1], 5, truncate(ann.Value, 18), "1", 0, "", false, 0, "")
			pdf.CellFormat(annColW[2], 5, truncate(ann.IstioEquiv, 42), "1", 0, "", false, 0, "")
			pdf.CellFormat(annColW[3], 5, truncate(ann.ManualAction, 52), "1", 0, "", false, 0, "")
			pdf.Ln(-1)
		}
		pdf.Ln(3)
	}

	filename := fmt.Sprintf("ingress-migration-report-%s.pdf", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := pdf.Output(w); err != nil {
		log.Printf("pdf write error: %v", err)
	}
}

// truncate shortens s to at most n runes, appending "..." if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
}
