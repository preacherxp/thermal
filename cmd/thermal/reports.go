package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"thermal-cli/internal/thermal"
)

// Saved-report commands accept flags before or after the input filenames.
func reportArgs(args []string, count int, allowJSON bool, usage string) ([]string, reportExports, bool, error) {
	var paths []string
	exports := reportExports{}
	asJSON, literal := false, false
	fail := func(err error) ([]string, reportExports, bool, error) { return nil, reportExports{}, false, err }
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
			return fail(flag.ErrHelp)
		case a == "--json" && allowJSON:
			asJSON = true
		case a == "--png" || strings.HasPrefix(a, "--png=") || a == "--pdf" || strings.HasPrefix(a, "--pdf="):
			name := strings.SplitN(a, "=", 2)[0]
			target := &exports.png
			if name == "--pdf" {
				target = &exports.pdf
			}
			if *target != "" {
				return fail(fmt.Errorf("%s may only be specified once", name))
			}
			if strings.Contains(a, "=") {
				*target = strings.SplitN(a, "=", 2)[1]
			} else {
				i++
				if i >= len(args) {
					return fail(fmt.Errorf("%s requires a file path", name))
				}
				*target = args[i]
			}
			if *target == "" {
				return fail(fmt.Errorf("%s requires a file path", name))
			}
		case strings.HasPrefix(a, "-"):
			return fail(fmt.Errorf("unknown option %s; %s", a, usage))
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) != count {
		return fail(errors.New(usage))
	}
	if exports.png != "" {
		if err := checkExportPath(exports.png, "png"); err != nil {
			return fail(err)
		}
	}
	if exports.pdf != "" {
		if err := checkExportPath(exports.pdf, "pdf"); err != nil {
			return fail(err)
		}
	}
	return paths, exports, asJSON, nil
}

func savedReport(args []string, out, errOut io.Writer) error {
	usage := "usage: thermal report RUN.json [--pdf FILE.pdf] [--png FILE.png]"
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
	return path.save(r, nil, errOut)
}
func compareReport(args []string, out, errOut io.Writer) error {
	usage := "usage: thermal compare BEFORE.json AFTER.json [--json] [--pdf FILE.pdf] [--png FILE.png]"
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
	return path.save(a, &b, errOut)
}
func demoReport(args []string, out, errOut io.Writer) error {
	usage := "usage: thermal demo [--pdf FILE.pdf] [--png FILE.png]"
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
	return path.save(a, &b, errOut)
}
