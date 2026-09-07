package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"thermal-cli/internal/thermal"
	"thermal-cli/internal/tui"
)

func captureDashboard(ctx context.Context, mode, target string, o thermal.Options, output string, pdf pdfFlags, png pngFlags, out, errOut io.Writer) (bool, error) {
	if !tui.Available(os.Stdin, out, errOut) {
		return false, nil
	}
	result, started, uiErr := tui.Run(ctx, os.Stdin, out.(*os.File), tui.Config{Mode: mode, Target: target, Duration: o.Duration.Seconds(), StopTemp: o.StopTemp}, func(ctx context.Context, progress func(string, thermal.Sample)) tui.Result {
		var r thermal.Run
		var err error
		if mode == "benchmark" {
			r, err = thermal.CaptureBenchmarks(ctx, thermal.NewSensors(), o, target, progress)
		} else {
			r, err = thermal.Capture(ctx, thermal.NewSensors(), o, func(s thermal.Sample) { progress("RECORD", s) })
		}
		if err != nil {
			return tui.Result{Run: r, Err: err}
		}
		path, err := thermal.Save(r, output)
		if err != nil {
			return tui.Result{Run: r, Err: fmt.Errorf("save run: %w", err)}
		}
		var saved bytes.Buffer
		fmt.Fprintln(&saved, "Saved "+path)
		pdfErr := pdf.save(path, r, &saved)
		pngErr := png.save(path, r, &saved)
		return tui.Result{Run: r, Saved: saved.String(), Err: errors.Join(pdfErr, pngErr)}
	})
	if !started {
		return false, nil
	}
	if uiErr != nil {
		fmt.Fprintln(errOut, "Live view closed; showing the saved result.")
		if mode == "benchmark" {
			thermal.BenchmarkSummary(out, result.Run)
		} else {
			thermal.Report(out, result.Run)
		}
		fmt.Fprint(errOut, result.Saved)
	}
	if result.Err != nil {
		return true, result.Err
	}
	if result.Run.Status != "complete" {
		return true, fmt.Errorf("run %s (results saved)", result.Run.Status)
	}
	return true, nil
}
