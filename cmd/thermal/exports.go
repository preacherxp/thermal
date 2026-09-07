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

type exportFlags struct {
	format   string
	path     *string
	disabled *bool
}

func addExportFlags(f *flag.FlagSet, format string) exportFlags {
	label := strings.ToUpper(format)
	return exportFlags{format, f.String(format, "", label+" report path; defaults to the JSON path with ."+format+" extension"), f.Bool("no-"+format, false, "Skip automatic "+label+" report")}
}
func defaultExportPath(jsonPath, format string) string {
	return strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + "." + format
}
func checkExportPath(path, format string) error {
	if !strings.EqualFold(filepath.Ext(path), "."+format) {
		return fmt.Errorf("--%s output must end in .%s", format, format)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (p exportFlags) validate(jsonPath string) error {
	if *p.disabled {
		if *p.path != "" {
			return fmt.Errorf("--%s and --no-%s cannot be combined", p.format, p.format)
		}
		return nil
	}
	path := *p.path
	if path == "" && jsonPath != "" {
		path = defaultExportPath(jsonPath, p.format)
	}
	if path == "" {
		return nil
	}
	if jsonPath != "" {
		a, err := filepath.Abs(jsonPath)
		if err != nil {
			return err
		}
		b, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if strings.EqualFold(a, b) {
			return fmt.Errorf("JSON and %s outputs must use different paths", strings.ToUpper(p.format))
		}
	}
	return checkExportPath(path, p.format)
}
func (p exportFlags) save(jsonPath string, r thermal.Run, w io.Writer) error {
	if *p.disabled {
		return nil
	}
	path := *p.path
	if path == "" {
		path = defaultExportPath(jsonPath, p.format)
	}
	if err := exportReport(p.format, path, r, nil, w); err != nil {
		return fmt.Errorf("JSON saved to %s, but %s export failed: %w", jsonPath, strings.ToUpper(p.format), err)
	}
	return nil
}
func exportReport(format, path string, a thermal.Run, b *thermal.Run, w io.Writer) error {
	if path == "" {
		return nil
	}
	save := thermal.SaveReportPDF
	if format == "png" {
		save = thermal.SaveReportPNG
	}
	if err := save(path, a, b); err != nil {
		return err
	}
	fmt.Fprintln(w, "Saved "+path)
	return nil
}

type reportExports struct{ png, pdf string }

func (p reportExports) save(a thermal.Run, b *thermal.Run, w io.Writer) error {
	pdfErr := exportReport("pdf", p.pdf, a, b, w)
	pngErr := exportReport("png", p.png, a, b, w)
	return errors.Join(pdfErr, pngErr)
}
