// Command dist builds and packages the standalone release binaries. Run from the repository root.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"thermal-cli/internal/thermal"
)

type target struct{ os, arch string }

var targets = []target{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

func (t target) name() string { return t.os + "-" + t.arch }
func (t target) binary() string {
	if t.os == "windows" {
		return "thermal.exe"
	}
	return "thermal"
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: go run ./scripts/dist build [--local] | check-deps | package")
	}
	switch args[0] {
	case "build":
		selected := targets
		if len(args) == 2 && args[1] == "--local" {
			selected = nil
			for _, t := range targets {
				if t.os == runtime.GOOS && t.arch == runtime.GOARCH {
					selected = append(selected, t)
				}
			}
			if len(selected) == 0 {
				return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
			}
		} else if len(args) != 1 {
			return fmt.Errorf("usage: build [--local]")
		}
		for _, t := range selected {
			dir := filepath.Join("dist", t.name())
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			fmt.Println("Building", t.name())
			if _, err := goCommand(t, "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(dir, t.binary()), "./cmd/thermal"); err != nil {
				return err
			}
		}
		return nil
	case "check-deps":
		if len(args) != 1 {
			return fmt.Errorf("check-deps takes no arguments")
		}
		modules, err := goCommand(target{runtime.GOOS, runtime.GOARCH}, "list", "-m", "all")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(modules)) != "thermal-cli" {
			return fmt.Errorf("external Go modules are forbidden: %s", modules)
		}
		for _, t := range targets {
			deps, err := goCommand(t, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./...")
			if err != nil {
				return err
			}
			for _, dep := range strings.Fields(string(deps)) {
				if !strings.HasPrefix(dep, "thermal-cli/") {
					return fmt.Errorf("non-standard dependency on %s: %s", t.name(), dep)
				}
			}
		}
		fmt.Println("Verified: standard-library-only Go, cgo disabled, all six targets.")
		return nil
	case "package":
		if len(args) != 1 {
			return fmt.Errorf("package takes no arguments")
		}
		return packageRelease()
	default:
		return fmt.Errorf("unknown distribution command %q", args[0])
	}
}

func goCommand(t target, args ...string) ([]byte, error) {
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	cmd.Env = append(os.Environ(), "GOOS="+t.os, "GOARCH="+t.arch, "CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return output, nil
}

type entry struct {
	name string
	data []byte
	mode int64
}

func archive(path string, entries []entry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if strings.HasSuffix(path, ".zip") {
		w := zip.NewWriter(f)
		for _, e := range entries {
			h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
			h.SetMode(os.FileMode(e.mode))
			var dst io.Writer
			dst, err = w.CreateHeader(h)
			if err == nil {
				_, err = dst.Write(e.data)
			}
			if err != nil {
				break
			}
		}
		if closeErr := w.Close(); err == nil {
			err = closeErr
		}
	} else {
		gz := gzip.NewWriter(f)
		w := tar.NewWriter(gz)
		for _, e := range entries {
			err = w.WriteHeader(&tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.data))})
			if err == nil {
				_, err = w.Write(e.data)
			}
			if err != nil {
				break
			}
		}
		if closeErr := w.Close(); err == nil {
			err = closeErr
		}
		if closeErr := gz.Close(); err == nil {
			err = closeErr
		}
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func packageRelease() error {
	tag := "v" + thermal.Version
	if ref := os.Getenv("GITHUB_REF"); strings.HasPrefix(ref, "refs/tags/") && ref != "refs/tags/"+tag {
		return fmt.Errorf("tag %s does not match binary version %s", ref, tag)
	}
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		repo = "preacherxp/thermal"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) {
		return fmt.Errorf("set GITHUB_REPOSITORY to the release repository (owner/repo)")
	}
	goLicense, err := os.ReadFile(filepath.Join(runtime.GOROOT(), "LICENSE"))
	if err != nil {
		return err
	}
	fontLicense, err := os.ReadFile("internal/thermal/assets/LICENSE")
	if err != nil {
		return err
	}
	notices := append([]byte("Third-party notices for the bundled Go binaries.\n\nGo runtime and standard library\n"), goLicense...)
	notices = append(notices, []byte("\nGo font (embedded PNG report atlas)\n")...)
	notices = append(notices, fontLicense...)
	common := []entry{{"THIRD_PARTY_NOTICES.txt", notices, 0644}}
	for _, name := range []string{"LICENSE", "README.md", "RUN.md", "AGENTS.md"} {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		data = []byte(strings.ReplaceAll(string(data), "preacherxp/thermal", repo))
		common = append(common, entry{name, data, 0644})
	}
	sample, err := os.ReadFile("examples/comparison.png")
	if err != nil {
		return err
	}
	common = append(common, entry{"examples/comparison.png", sample, 0644})
	// Read every binary before writing any assets, so incomplete builds fail early.
	binaries := make(map[target][]byte)
	for _, t := range targets {
		data, err := os.ReadFile(filepath.Join("dist", t.name(), t.binary()))
		if err != nil {
			return fmt.Errorf("run go run ./scripts/dist build first: %w", err)
		}
		if len(data) == 0 {
			return fmt.Errorf("empty binary for %s", t.name())
		}
		binaries[t] = data
	}
	// A fresh version directory prevents accidental replacement or stale release assets.
	if err := os.MkdirAll("release", 0755); err != nil {
		return err
	}
	dir := filepath.Join("release", tag)
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	var names []string
	for _, t := range targets {
		ext := ".tar.gz"
		if t.os == "windows" {
			ext = ".zip"
		}
		name := "thermal-" + t.name() + ext
		entries := append([]entry{{t.binary(), binaries[t], 0755}}, common...)
		if err := archive(filepath.Join(dir, name), entries); err != nil {
			return err
		}
		names = append(names, name)
	}
	for _, name := range []string{"install.sh", "install.ps1"} {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		data = []byte(strings.ReplaceAll(string(data), "preacherxp/thermal", repo))
		data = []byte(strings.ReplaceAll(string(data), "@VERSION@", tag))
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			return err
		}
		names = append(names, name)
	}
	var sums strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	fmt.Println("Release assets:", dir)
	return nil
}
