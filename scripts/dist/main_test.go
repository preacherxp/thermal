package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageRelease(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(root) })
	t.Setenv("GITHUB_REPOSITORY", "test-owner/thermal-fork")
	t.Setenv("GITHUB_REF", "refs/tags/v0.5.0")
	for _, name := range []string{"LICENSE", "README.md", "RUN.md", "AGENTS.md", "internal/thermal/assets/LICENSE", "install.sh", "install.ps1", "examples/comparison.png"} {
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("preacherxp/thermal @VERSION@"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := packageRelease(); err == nil {
		t.Fatal("packaged without binaries")
	}
	for _, target := range targets {
		dir := filepath.Join("dist", target.name())
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, target.binary()), []byte(target.name()), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := packageRelease(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("release", "v0.5.0")
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(checksums)), "\n")
	if len(lines) != 8 {
		t.Fatalf("expected six archives and two installers, got %d", len(lines))
	}
	for _, line := range lines {
		parts := strings.Fields(line)
		data, err := os.ReadFile(filepath.Join(dir, parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != parts[0] {
			t.Fatalf("incorrect checksum: %s", parts[1])
		}
	}
	for _, target := range targets {
		files := map[string][]byte{}
		if target.os == "windows" {
			z, err := zip.OpenReader(filepath.Join(dir, "thermal-"+target.name()+".zip"))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range z.File {
				r, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				files[file.Name], err = io.ReadAll(r)
				_ = r.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			_ = z.Close()
		} else {
			f, err := os.Open(filepath.Join(dir, "thermal-"+target.name()+".tar.gz"))
			if err != nil {
				t.Fatal(err)
			}
			gz, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			r := tar.NewReader(gz)
			for {
				h, err := r.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				files[h.Name], err = io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				if h.Name == "thermal" && h.Mode != 0755 {
					t.Fatal("executable permissions lost")
				}
			}
			_ = gz.Close()
			_ = f.Close()
		}
		if len(files) != 7 || string(files[target.binary()]) != target.name() {
			t.Fatalf("incorrect archive for %s: %v", target.name(), files)
		}
		if !strings.Contains(string(files["README.md"]), "test-owner/thermal-fork") {
			t.Fatal("repository not substituted in docs")
		}
	}
	for _, name := range []string{"install.sh", "install.ps1"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "test-owner/thermal-fork v0.5.0" {
			t.Fatalf("installer not pinned: %s", data)
		}
	}
	if err := packageRelease(); err == nil {
		t.Fatal("overwrote release")
	}
	t.Setenv("GITHUB_REF", "refs/tags/v9.9.9")
	if err := packageRelease(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("accepted version mismatch: %v", err)
	}
}
