package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thermal-cli/internal/thermal"
)

func assertPDF(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-1.4")) || !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Fatalf("invalid PDF %s: %v", path, err)
	}
}
func TestAutomaticPDFImport(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "input.csv")
	if err := os.WriteFile(csv, []byte("sec,temp_C\n0,40\n5,55\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, skip := range []bool{false, true} {
		name := "default"
		if skip {
			name = "disabled"
		}
		path := filepath.Join(dir, name+".json")
		args := []string{"import", "--out", path, "--no-png"}
		if skip {
			args = append(args, "--no-pdf")
		}
		args = append(args, csv)
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr) != 0 {
			t.Fatal(stderr.String())
		}
		if skip {
			if _, err := os.Stat(defaultExportPath(path, "pdf")); !os.IsNotExist(err) {
				t.Fatal("--no-pdf was ignored")
			}
		} else {
			assertPDF(t, defaultExportPath(path, "pdf"))
			if !strings.Contains(stderr.String(), defaultExportPath(path, "pdf")) {
				t.Fatal("PDF path not announced")
			}
		}
	}
}
func TestPDFExportCommandsAndJSON(t *testing.T) {
	dir := t.TempDir()
	a, b := demo()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	if _, err := thermal.Save(a, before); err != nil {
		t.Fatal(err)
	}
	if _, err := thermal.Save(b, after); err != nil {
		t.Fatal(err)
	}
	tests := [][]string{
		{"report", before, "--pdf", filepath.Join(dir, "report.pdf")},
		{"compare", "--pdf=" + filepath.Join(dir, "compare.pdf"), before, after, "--json", "--png", filepath.Join(dir, "compare.png")},
		{"demo", "--pdf", filepath.Join(dir, "demo.pdf")},
	}
	for _, args := range tests {
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr) != 0 {
			t.Fatalf("%v: %s", args, stderr.String())
		}
		if args[0] == "compare" && !json.Valid(out.Bytes()) {
			t.Fatal("PDF export corrupted JSON stdout")
		}
	}
	for _, name := range []string{"report", "compare", "demo"} {
		assertPDF(t, filepath.Join(dir, name+".pdf"))
	}
	var out, stderr bytes.Buffer
	if run(tests[0], &out, &stderr) != 1 {
		t.Fatal("existing PDF overwritten")
	}
}
func TestPDFValidationBeforeLoad(t *testing.T) {
	for _, args := range [][]string{
		{"record", "--pdf", "report.pdf", "--no-pdf"},
		{"benchmark", "--pdf", "wrong.png"},
		{"record", "--out", "same.pdf", "--pdf", "same.pdf"},
		{"report", "missing.json", "--pdf"},
		{"demo", "--pdf="},
		{"demo", "--pdf", "one.pdf", "--pdf", "two.pdf"},
	} {
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr) != 1 {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestPDFExportFailureKeepsSavedJSON(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "input.csv")
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(csv, []byte("sec,temp_C\n0,40\n5,55\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "saved.json")
	data := demoRunForPDFTest()
	if _, err := thermal.Save(data, path); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(blocker, "report.pdf")
	disabled := false
	flags := exportFlags{format: "pdf", path: &pdfPath, disabled: &disabled}
	var stderr bytes.Buffer
	if err := flags.save(path, data, &stderr); err == nil || !strings.Contains(err.Error(), "JSON saved") {
		t.Fatalf("missing saved-data recovery error: %v", err)
	}
	if _, err := thermal.Load(path); err != nil {
		t.Fatal("JSON was lost")
	}
	good := filepath.Join(dir, "good.pdf")
	exports := reportExports{pdf: good, png: filepath.Join(blocker, "bad.png")}
	if err := exports.save(data, nil, &stderr); err == nil {
		t.Fatal("PNG error not returned")
	}
	assertPDF(t, good)
}
func demoRunForPDFTest() thermal.Run { a, _ := demo(); return a }
