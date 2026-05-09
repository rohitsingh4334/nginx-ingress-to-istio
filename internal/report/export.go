package report

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/xuri/excelize/v2"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/istio"
)

func (s *Server) handleExportExcel(w http.ResponseWriter, r *http.Request) {
	f := excelize.NewFile()
	defer f.Close()

	f.SetSheetName(f.GetSheetName(0), "Summary")
	headers := []string{"Name", "Namespace", "Complexity", "Hosts", "TLS", "Warnings"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue("Summary", cell, h)
	}
	styleHeader, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"4472C4"}, Pattern: 1},
	})
	f.SetCellStyle("Summary", "A1", "F1", styleHeader)
	complexityColors := map[analyzer.Complexity]string{Low: "C6EFCE", Medium: "FFEB9C", High: "FFC7CE"}

	for row, res := range s.results {
		r := row + 2
		f.SetCellValue("Summary", fmt.Sprintf("A%d", r), res.Name)
		f.SetCellValue("Summary", fmt.Sprintf("B%d", r), res.Namespace)
		f.SetCellValue("Summary", fmt.Sprintf("C%d", r), string(res.Complexity))
		f.SetCellValue("Summary", fmt.Sprintf("D%d", r), strings.Join(res.Hosts, ", "))
		f.SetCellValue("Summary", fmt.Sprintf("E%d", r), res.TLSEnabled)
		f.SetCellValue("Summary", fmt.Sprintf("F%d", r), strings.Join(res.Warnings, "; "))
		if color, ok := complexityColors[res.Complexity]; ok {
			style, _ := f.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1}})
			f.SetCellStyle("Summary", fmt.Sprintf("C%d", r), fmt.Sprintf("C%d", r), style)
		}
	}
	f.SetColWidth("Summary", "A", "F", 25)

	f.NewSheet("Annotations")
	annHeaders := []string{"Ingress", "Namespace", "Annotation", "Value", "Istio Equivalent", "Manual Action"}
	for i, h := range annHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue("Annotations", cell, h)
	}
	f.SetCellStyle("Annotations", "A1", "F1", styleHeader)
	annRow := 2
	for _, res := range s.results {
		for _, ann := range res.Annotations {
			f.SetCellValue("Annotations", fmt.Sprintf("A%d", annRow), res.Name)
			f.SetCellValue("Annotations", fmt.Sprintf("B%d", annRow), res.Namespace)
			f.SetCellValue("Annotations", fmt.Sprintf("C%d", annRow), ann.Annotation)
			f.SetCellValue("Annotations", fmt.Sprintf("D%d", annRow), ann.Value)
			f.SetCellValue("Annotations", fmt.Sprintf("E%d", annRow), ann.IstioEquiv)
			f.SetCellValue("Annotations", fmt.Sprintf("F%d", annRow), ann.ManualAction)
			annRow++
		}
	}
	f.SetColWidth("Annotations", "A", "F", 30)

	f.NewSheet("Istio Resources")
	for i, h := range []string{"Ingress", "Namespace", "Gateway YAML", "VirtualService YAML", "DestinationRule YAML"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue("Istio Resources", cell, h)
	}
	f.SetCellStyle("Istio Resources", "A1", "E1", styleHeader)
	for row, res := range s.results {
		ir := istio.Generate(res)
		r := row + 2
		f.SetCellValue("Istio Resources", fmt.Sprintf("A%d", r), res.Name)
		f.SetCellValue("Istio Resources", fmt.Sprintf("B%d", r), res.Namespace)
		f.SetCellValue("Istio Resources", fmt.Sprintf("C%d", r), ir.Gateway)
		f.SetCellValue("Istio Resources", fmt.Sprintf("D%d", r), ir.VirtualService)
		f.SetCellValue("Istio Resources", fmt.Sprintf("E%d", r), ir.DestinationRule)
	}
	f.SetColWidth("Istio Resources", "A", "E", 50)

	filename := fmt.Sprintf("ingress-migration-report-%s.xlsx", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	f.Write(w)
}

func (s *Server) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, 10, "NGINX Ingress to Istio Migration Report", "", 1, "C", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	pdf.CellFormat(0, 6, fmt.Sprintf("Generated: %s", time.Now().Format("2006-01-02 15:04:05")), "", 1, "C", false, 0, "")
	pdf.Ln(4)

	colW := []float64{50, 35, 30, 65, 15, 75}
	pdf.SetFillColor(68, 114, 196)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 8)
	for i, h := range []string{"Name", "Namespace", "Complexity", "Hosts", "TLS", "Warnings"} {
		pdf.CellFormat(colW[i], 7, h, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 7)

	rgb := map[analyzer.Complexity][3]int{Low: {198, 239, 206}, Medium: {255, 235, 156}, High: {255, 199, 206}}
	for _, res := range s.results {
		c := rgb[res.Complexity]
		pdf.SetFillColor(255, 255, 255)
		pdf.CellFormat(colW[0], 6, truncate(res.Name, 30), "1", 0, "", false, 0, "")
		pdf.CellFormat(colW[1], 6, truncate(res.Namespace, 20), "1", 0, "", false, 0, "")
		pdf.SetFillColor(int(c[0]), int(c[1]), int(c[2]))
		pdf.CellFormat(colW[2], 6, string(res.Complexity), "1", 0, "C", true, 0, "")
		pdf.SetFillColor(255, 255, 255)
		tls := "No"
		if res.TLSEnabled { tls = "Yes" }
		pdf.CellFormat(colW[3], 6, truncate(strings.Join(res.Hosts, ", "), 40), "1", 0, "", false, 0, "")
		pdf.CellFormat(colW[4], 6, tls, "1", 0, "C", false, 0, "")
		pdf.CellFormat(colW[5], 6, truncate(strings.Join(res.Warnings, "; "), 50), "1", 0, "", false, 0, "")
		pdf.Ln(-1)
	}

	filename := fmt.Sprintf("ingress-migration-report-%s.pdf", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	pdf.Output(w)
}

func truncate(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n-1] + "…"
}
