package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"thermal-cli/internal/thermal"
)

type pdfFlags struct {
	path     *string
	disabled *bool
}

func addPDFFlags(f *flag.FlagSet) pdfFlags {
	return pdfFlags{f.String("pdf", "", "PDF findings report path; defaults to the JSON path with .pdf extension"), f.Bool("no-pdf", false, "Skip automatic PDF findings report")}
}
func defaultPDFPath(jsonPath string) string {
	return strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".pdf"
}
func checkPDFPath(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		return errors.New("--pdf output must end in .pdf")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (p pdfFlags) validate(jsonPath string) error {
	if *p.disabled {
		if *p.path != "" {
			return errors.New("--pdf and --no-pdf cannot be combined")
		}
		return nil
	}
	path := *p.path
	if path == "" && jsonPath != "" {
		path = defaultPDFPath(jsonPath)
	}
	if path == "" {
		return nil
	}
	if jsonPath != "" {
		a, _ := filepath.Abs(jsonPath)
		b, _ := filepath.Abs(path)
		if strings.EqualFold(a, b) {
			return errors.New("JSON and PDF outputs must use different paths")
		}
	}
	return checkPDFPath(path)
}
func (p pdfFlags) save(jsonPath string, r thermal.Run, w io.Writer) error {
	if *p.disabled {
		return nil
	}
	path := *p.path
	if path == "" {
		path = defaultPDFPath(jsonPath)
	}
	if err := thermal.SaveReportPDF(path, r, nil); err != nil {
		return fmt.Errorf("JSON saved to %s, but PDF export failed: %w", jsonPath, err)
	}
	fmt.Fprintln(w, "Saved "+path)
	return nil
}
func exportPDF(path string, a thermal.Run, b *thermal.Run, w io.Writer) error {
	if path == "" {
		return nil
	}
	if err := thermal.SaveReportPDF(path, a, b); err != nil {
		return err
	}
	fmt.Fprintln(w, "Saved "+path)
	return nil
}

type reportExports struct{ png, pdf string }

func (p reportExports) save(a thermal.Run, b *thermal.Run, w io.Writer) error {
	// Attempt both independent exports, preserving a successful report if the other fails.
	pdfErr := exportPDF(p.pdf, a, b, w)
	pngErr := exportPNG(p.png, a, b, w)
	return errors.Join(pdfErr, pngErr)
}
