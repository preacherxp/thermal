package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"thermal-cli/internal/thermal"
)

func assertPNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := png.DecodeConfig(f); err != nil {
		t.Fatal(err)
	}
}
func TestPNGCLI(t *testing.T) {
	dir := t.TempDir()
	a, b := demo()
	before := filepath.Join(dir, "before.json")
	after := filepath.Join(dir, "after.json")
	if _, err := thermal.Save(a, before); err != nil {
		t.Fatal(err)
	}
	if _, err := thermal.Save(b, after); err != nil {
		t.Fatal(err)
	}
	for i, args := range [][]string{
		{"report", before, "--png"}, {"compare", before, after, "--json", "--png"}, {"demo", "--png"},
	} {
		path := filepath.Join(dir, []string{"report.png", "comparison.png", "demo.png"}[i])
		args = append(args, path)
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr) != 0 {
			t.Fatal(stderr.String())
		}
		assertPNG(t, path)
		if i == 1 && !json.Valid(out.Bytes()) {
			t.Fatal("PNG export corrupted JSON stdout")
		}
		if run(args, &out, &stderr) != 1 {
			t.Fatal("overwrote PNG")
		}
	}
	var out, stderr bytes.Buffer
	path := filepath.Join(dir, "prefix.png")
	if run([]string{"report", "--png=" + path, before}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	assertPNG(t, path)
}
func TestImportAutomaticPNG(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "input.csv")
	if err := os.WriteFile(csv, []byte("sec,temp_C\n0,40\n5,50\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, skip := range []bool{false, true} {
		name := "automatic"
		if skip {
			name = "skip"
		}
		path := filepath.Join(dir, name+".json")
		args := []string{"import", "--out", path}
		if skip {
			args = append(args, "--no-png")
		}
		args = append(args, csv)
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr) != 0 {
			t.Fatal(stderr.String())
		}
		if skip {
			if _, err := os.Stat(defaultPNGPath(path)); !os.IsNotExist(err) {
				t.Fatal("--no-png ignored")
			}
		} else {
			assertPNG(t, defaultPNGPath(path))
		}
	}
}
func TestPNGInvalidOptionsBeforeCapture(t *testing.T) {
	for _, args := range [][]string{
		{"record", "--png", "report.png", "--no-png"},
		{"benchmark", "--png", "run.json"},
		{"record", "--out", "same.png", "--png", "same.png"},
		{"report", "missing.json", "--png"},
		{"demo", "--png="}, {"demo", "unexpected"},
	} {
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr) != 1 {
			t.Fatalf("accepted %v", args)
		}
	}
}
