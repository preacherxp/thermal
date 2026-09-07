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

type pngFlags struct {
	path     *string
	disabled *bool
}

func addPNGFlags(f *flag.FlagSet) pngFlags {
	return pngFlags{f.String("png", "", "PNG report path; defaults to the JSON path with .png extension"), f.Bool("no-png", false, "Skip automatic PNG report")}
}
func defaultPNGPath(jsonPath string) string {
	return strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".png"
}
func checkPNGPath(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return errors.New("--png output must end in .png")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (p pngFlags) validate(jsonPath string) error {
	if *p.disabled {
		if *p.path != "" {
			return errors.New("--png and --no-png cannot be combined")
		}
		return nil
	}
	path := *p.path
	if path == "" && jsonPath != "" {
		path = defaultPNGPath(jsonPath)
	}
	if path != "" {
		if jsonPath != "" {
			a, _ := filepath.Abs(jsonPath)
			b, _ := filepath.Abs(path)
			if strings.EqualFold(a, b) {
				return errors.New("JSON and PNG outputs must use different paths")
			}
		}
		return checkPNGPath(path)
	}
	return nil
}
func (p pngFlags) save(jsonPath string, r thermal.Run, w io.Writer) error {
	if *p.disabled {
		return nil
	}
	path := *p.path
	if path == "" {
		path = defaultPNGPath(jsonPath)
	}
	if err := thermal.SaveReportPNG(path, r, nil); err != nil {
		return fmt.Errorf("JSON saved to %s, but PNG export failed: %w", jsonPath, err)
	}
	fmt.Fprintln(w, "Saved "+path)
	return nil
}

// Saved-report commands accept flags before or after the input filenames.
func reportArgs(args []string, count int, allowJSON bool, usage string) ([]string, string, bool, error) {
	var paths []string
	pngPath := ""
	asJSON := false
	literal := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if literal {
			paths = append(paths, a)
			continue
		}
		switch {
		case a == "--":
			literal = true
		case a == "--help" || a == "-h":
			return nil, "", false, flag.ErrHelp
		case a == "--json" && allowJSON:
			asJSON = true
		case a == "--png" || strings.HasPrefix(a, "--png="):
			if pngPath != "" {
				return nil, "", false, errors.New("--png may only be specified once")
			}
			if a == "--png" {
				i++
				if i >= len(args) {
					return nil, "", false, errors.New("--png requires a FILE.png path")
				}
				pngPath = args[i]
			} else {
				pngPath = strings.TrimPrefix(a, "--png=")
			}
			if pngPath == "" {
				return nil, "", false, errors.New("--png requires a FILE.png path")
			}
		case strings.HasPrefix(a, "-"):
			return nil, "", false, fmt.Errorf("unknown option %s; %s", a, usage)
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) != count {
		return nil, "", false, errors.New(usage)
	}
	if pngPath != "" {
		if err := checkPNGPath(pngPath); err != nil {
			return nil, "", false, err
		}
	}
	return paths, pngPath, asJSON, nil
}
func exportPNG(path string, a thermal.Run, b *thermal.Run, w io.Writer) error {
	if path == "" {
		return nil
	}
	if err := thermal.SaveReportPNG(path, a, b); err != nil {
		return err
	}
	fmt.Fprintln(w, "Saved "+path)
	return nil
}
func savedReport(args []string, out, errOut io.Writer) error {
	usage := "usage: thermal report RUN.json [--png FILE.png]"
	paths, path, _, err := reportArgs(args, 1, false, usage)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(out, usage)
		return nil
	}
	if err != nil {
		return err
	}
	r, err := thermal.Load(paths[0])
	if err != nil {
		return err
	}
	thermal.Report(out, r)
	return exportPNG(path, r, nil, errOut)
}
func compareReport(args []string, out, errOut io.Writer) error {
	usage := "usage: thermal compare BEFORE.json AFTER.json [--json] [--png FILE.png]"
	paths, path, asJSON, err := reportArgs(args, 2, true, usage)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(out, usage)
		return nil
	}
	if err != nil {
		return err
	}
	a, err := thermal.Load(paths[0])
	if err != nil {
		return err
	}
	b, err := thermal.Load(paths[1])
	if err != nil {
		return err
	}
	c := thermal.Compare(a, b)
	if asJSON {
		if err := writeJSON(out, c); err != nil {
			return err
		}
	} else {
		thermal.PrintComparison(out, a, b, c)
	}
	return exportPNG(path, a, &b, errOut)
}
func demoReport(args []string, out, errOut io.Writer) error {
	usage := "usage: thermal demo [--png FILE.png]"
	_, path, _, err := reportArgs(args, 0, false, usage)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(out, usage)
		return nil
	}
	if err != nil {
		return err
	}
	a, b := demo()
	thermal.PrintComparison(out, a, b, thermal.Compare(a, b))
	return exportPNG(path, a, &b, errOut)
}
