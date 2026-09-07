package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"thermal-cli/internal/thermal"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		help(out)
		return 0
	}
	// A bare invocation (or benchmark flags alone) tests CPU and GPU without prompts.
	// Explicit subcommands retain their existing defaults.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") && args[0] != "--version" {
		args = append([]string{"benchmark", "--survey", "skip"}, args...)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var err error
	switch args[0] {
	case "__gpu-worker":
		if len(args) != 2 {
			return 1
		}
		duration, e := time.ParseDuration(args[1])
		if e == nil {
			e = thermal.RunGPUWorker(ctx, duration, os.Stdin, out)
		}
		if e != nil {
			thermal.GPUWorkerError(out, e)
			return 1
		}
	case "version", "--version":
		fmt.Fprintln(out, "thermal "+thermal.Version)
	case "stages":
		for i, s := range thermal.Stages {
			fmt.Fprintf(out, "%d. %s\n", i, s)
		}
	case "survey":
		err = surveyCommand(ctx, args[1:], out, errOut)
	case "doctor":
		err = doctor(ctx, args[1:], out, errOut)
	case "record", "benchmark":
		err = capture(ctx, args[0], args[1:], out, errOut)
	case "report":
		err = savedReport(args[1:], out, errOut)
	case "compare":
		err = compareReport(args[1:], out, errOut)
	case "history":
		err = history(out)
	case "import":
		err = importCSV(args[1:], out, errOut)
	case "demo":
		err = demoReport(args[1:], out, errOut)
	default:
		err = fmt.Errorf("unknown command %q; use thermal --help", args[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(errOut, "thermal:", err)
		return 1
	}
	return 0
}
func help(w io.Writer) {
	fmt.Fprint(w, `THERMAL  -  measure, change one thing, compare

Usage: thermal [options]
       thermal <command> [options]

Run thermal with no arguments to benchmark CPU, then GPU, for 30 seconds each.
It saves JSON, PDF findings, and PNG results automatically without prompts.
Unavailable tests are marked in the report; missing temperatures require
--allow-unmonitored to run without temperature monitoring.

  survey       Identify hardware, rated watts, materials and score references
  doctor       Detect sensors and explain platform setup
  record       Monitor an idle or external workload and save a run
  benchmark    Test CPU and GPU (--target cpu|gpu|both; default both)
  compare      Compare BEFORE.json AFTER.json [--json] [--pdf FILE.pdf] [--png FILE.png]
  report       Show a saved RUN.json [--pdf FILE.pdf] [--png FILE.png]
  history      List saved runs
  stages       List intervention stages
  import       Import an existing NVIDIA thermal CSV
  demo         Synthetic before/after comparison [--pdf FILE.pdf] [--png FILE.png] (no load)
  version      Print version

Start:
  thermal
  thermal --duration 60s --out run.json
  thermal --json --no-png --no-pdf

Other tasks:
  thermal doctor --json
  thermal benchmark --survey skip --duration 30s
  thermal compare before.json after.json --png comparison.png

Interactive captures open a live dashboard. --no-tui uses minimal console output.
Redirected output and --json use the minimal path; --verbose shows full tables.
Run thermal record --help or thermal benchmark --help for options.
Captures and imports save PDF and PNG beside the JSON; --no-pdf / --no-png disable them.
GPU compute uses OpenCL on macOS and Windows; other platforms report unavailable.
macOS reads native AppleSMC temperatures; GPU monitoring supports Apple Silicon.
Apple Silicon CPU/GPU watts use native IOReport energy-model estimates.
Use record for monitoring without built-in load.
`)
}
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
func flags(name string, w io.Writer) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(w)
	return f
}
func doctor(ctx context.Context, args []string, out, errOut io.Writer) error {
	f := flags("doctor", errOut)
	asJSON := f.Bool("json", false, "Output JSON")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("doctor does not accept positional arguments")
	}
	s, w := thermal.Snapshot(ctx, thermal.NewSensors())
	if *asJSON {
		return writeJSON(out, struct {
			OS       string         `json:"os"`
			Sample   thermal.Sample `json:"sample"`
			Warnings []string       `json:"warnings"`
		}{runtime.GOOS, s, w})
	}
	thermal.PrintSnapshot(out, s, w, fmt.Sprintf("%s/%s · %d logical CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()))
	return nil
}
func capture(ctx context.Context, mode string, args []string, out, errOut io.Writer) error {
	f := flags(mode, errOut)
	stage := f.String("stage", "baseline", "Intervention stage: "+thermal.StageHelp())
	duration := f.Duration("duration", 30*time.Second, "Recording duration or duration per benchmark target (1s to 30m)")
	interval := f.Duration("interval", time.Second, "Sampling interval (200ms to 30s; providers may be slower)")
	profile := f.String("profile", "unknown", "Current OS/OEM thermal profile label")
	power := f.String("power", "unknown", "Power source: ac, battery, or unknown")
	ambient := f.String("ambient", "", "Room temperature in Celsius")
	notes := f.String("notes", "", "What changed or what you observed")
	workload := f.String("workload", "idle", "External workload label (record only)")
	workers := f.Int("workers", min(4, runtime.NumCPU()), "CPU benchmark workers")
	stop := f.Float64("stop-temp", 90, "Stop CPU load at this temperature in Celsius (50 to 100)")
	unmonitored := f.Bool("allow-unmonitored", false, "Explicitly allow benchmark load without a readable target temperature")
	output := f.String("out", "", "Run JSON path; defaults to user config thermal/runs")
	asJSON := f.Bool("json", false, "Print JSON instead of a human report")
	noTUI := f.Bool("no-tui", false, "Use minimal console output instead of the live dashboard")
	verbose := f.Bool("verbose", false, "Show full sensor tables and recommendations after capture")
	target := "both"
	if mode == "benchmark" {
		f.StringVar(&target, "target", "both", "Benchmark target: cpu, gpu, or both (sequential)")
	}
	pngOptions := addExportFlags(f, "png")
	pdfOptions := addExportFlags(f, "pdf")
	surveyOptions := addSurveyFlags(f)
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	if mode == "benchmark" && target != "cpu" && target != "gpu" && target != "both" {
		return errors.New("--target must be cpu, gpu, or both")
	}
	if err := pngOptions.validate(*output); err != nil {
		return err
	}
	if err := pdfOptions.validate(*output); err != nil {
		return err
	}
	if *power != "ac" && *power != "battery" && *power != "unknown" {
		return errors.New("--power must be ac, battery, or unknown")
	}
	var room *float64
	if *ambient != "" {
		v, e := strconv.ParseFloat(*ambient, 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("invalid --ambient")
		}
		room = &v
	}
	if math.IsNaN(*stop) || math.IsInf(*stop, 0) {
		return errors.New("invalid --stop-temp")
	}
	o := thermal.Options{Stage: *stage, Duration: *duration, Interval: *interval, Profile: *profile, PowerSource: *power, Ambient: room, Notes: *notes, Workload: *workload, Workers: *workers, StopTemp: *stop, AllowUnmonitored: *unmonitored, Benchmark: mode == "benchmark"}
	if err := o.Validate(); err != nil {
		return err
	}
	if *output != "" {
		if _, err := os.Stat(*output); err == nil {
			return fmt.Errorf("output already exists: %s", *output)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	prepared, e := surveyOptions.prepare(ctx, *profile, os.Stdin, errOut, *asJSON)
	if e != nil {
		return e
	}
	o.Preparation = prepared
	if prepared != nil {
		o.Profile = prepared.Profile
	}
	work := captureWork(mode, target, o, *output, pdfOptions, pngOptions)
	if !*noTUI && !*asJSON && !*verbose {
		if handled, err := captureDashboard(ctx, mode, target, o, out, errOut, work); handled {
			return err
		}
	}
	fmt.Fprintf(errOut, "Preparing %s (%s). Ctrl+C stops and saves partial results.\n", mode, duration.String())
	if mode == "benchmark" {
		fmt.Fprintf(errOut, "Benchmark %s: %s per test, %d CPU workers, stop limit %.1f C. Temperature checks are best effort, not hardware protection.\n", target, duration.String(), *workers, *stop)
	}
	progress, finish := thermal.NewProgress(errOut, duration.Seconds())
	phase := ""
	result := work(ctx, func(kind string, s thermal.Sample) {
		if mode == "benchmark" && phase != kind {
			finish()
			fmt.Fprintln(errOut, kind+" benchmark")
			phase = kind
		}
		progress(s)
	})
	finish()
	if result.Saved == "" {
		return result.Err
	}
	r := result.Run
	var err error
	if *asJSON {
		err = writeJSON(out, r)
	} else if mode == "benchmark" && !*verbose {
		thermal.BenchmarkSummary(out, r)
	} else {
		thermal.Report(out, r)
	}
	fmt.Fprint(errOut, result.Saved)
	if err != nil {
		return err
	}
	if result.Err != nil {
		return result.Err
	}
	if r.Status != "complete" {
		return fmt.Errorf("run %s (results saved)", r.Status)
	}
	return nil
}
func history(out io.Writer) error {
	paths, err := filepath.Glob(filepath.Join(thermal.DataDir(), "*.json"))
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	if len(paths) == 0 {
		fmt.Fprintln(out, "No saved runs in "+thermal.DataDir())
		return nil
	}
	for _, p := range paths {
		r, e := thermal.Load(p)
		if e != nil {
			fmt.Fprintf(out, "Unreadable: %s\n", p)
			continue
		}
		fmt.Fprintf(out, "%s  %-16s %-14s %s\n", r.Created.Format("2006-01-02 15:04"), r.Stage, r.Status, p)
	}
	return nil
}
func importCSV(args []string, out, errOut io.Writer) error {
	f := flags("import", errOut)
	stage := f.String("stage", "baseline", "Intervention stage")
	output := f.String("out", "", "Output JSON file")
	device := f.String("device", "imported-gpu", "Stable GPU identifier, shared between before/after imports")
	profile := f.String("profile", "unknown", "Recorded thermal profile")
	workload := f.String("workload", "external-gpu", "Original workload label")
	pngOptions := addExportFlags(f, "png")
	pdfOptions := addExportFlags(f, "pdf")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 || !thermal.ValidStage(*stage) {
		return errors.New("usage: thermal import [--stage fans-cleaned] [--out RUN.json] FILE.csv")
	}
	if err := pngOptions.validate(*output); err != nil {
		return err
	}
	if err := pdfOptions.validate(*output); err != nil {
		return err
	}
	h, err := os.Open(f.Arg(0))
	if err != nil {
		return err
	}
	defer h.Close()
	reader := csv.NewReader(h)
	header, err := reader.Read()
	if err != nil {
		return err
	}
	cols := map[string]int{}
	for i, v := range header {
		cols[strings.TrimSpace(strings.TrimPrefix(v, "\ufeff"))] = i
	}
	for _, key := range []string{"sec", "temp_C"} {
		if _, ok := cols[key]; !ok {
			return fmt.Errorf("CSV missing %s column", key)
		}
	}
	r := thermal.NewRun()
	r.Host = "imported"
	r.OS = "unknown"
	r.Arch = "unknown"
	r.CPUs = 0
	r.Stage = *stage
	r.Profile = *profile
	r.PowerSource = "unknown"
	r.Workload = *workload
	r.Warnings = []string{"Imported CSV: creation time is import time; original machine and conditions are unverified"}
	last := -1.0
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		get := func(k string) string {
			i, ok := cols[k]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		number := func(k string) *float64 {
			v, e := strconv.ParseFloat(get(k), 64)
			if e != nil {
				return nil
			}
			return thermal.Number(v)
		}
		sec, temp := number("sec"), number("temp_C")
		if sec == nil || temp == nil || *sec < 0 || *sec <= last || *temp < -30 || *temp > 150 {
			return fmt.Errorf("invalid CSV sample after %.1fs", last)
		}
		last = *sec
		d := thermal.Device{ID: "import:" + *device, Name: *device, Kind: "gpu", Source: "csv", Temp: temp, Util: number("util_pct"), Power: number("power_W"), Clock: number("clock_MHz")}
		sw, hw := get("sw_thermal"), get("hw_thermal")
		if sw == "Active" || hw == "Active" {
			v := true
			d.Throttled = &v
		} else if sw == "Not Active" && hw == "Not Active" {
			v := false
			d.Throttled = &v
		}
		r.Samples = append(r.Samples, thermal.Sample{Seconds: *sec, Devices: []thermal.Device{d}})
	}
	if len(r.Samples) == 0 {
		return errors.New("CSV contains no samples")
	}
	r.Duration = last - r.Samples[0].Seconds
	r.Elapsed = r.Duration
	if len(r.Samples) > 1 {
		r.Interval = (last - r.Samples[0].Seconds) / float64(len(r.Samples)-1)
	}
	path, err := thermal.Save(r, *output)
	if err != nil {
		return err
	}
	fmt.Fprintln(errOut, "Saved "+path)
	thermal.Report(out, r)
	pdfErr := pdfOptions.save(path, r, errOut)
	pngErr := pngOptions.save(path, r, errOut)
	return errors.Join(pdfErr, pngErr)
}
func demo() (thermal.Run, thermal.Run) {
	a := thermal.NewRun()
	a.Stage = "baseline"
	a.Host = "demo"
	a.Profile = "performance"
	a.PowerSource = "ac"
	a.Ambient = thermal.Number(22)
	a.Workload = "synthetic-gpu-example"
	a.Duration = 120
	a.Interval = 5
	b := a
	b.Stage = "fans-cleaned"
	for i := 0; i <= 24; i++ {
		for _, r := range []*thermal.Run{&a, &b} {
			power, clock, temp := 78.0, 1563.0, 87.0
			if r == &b {
				power = 104
				clock = 1882
			}
			if i == 0 {
				temp = 62
			}
			throttle := i > 2
			r.Samples = append(r.Samples, thermal.Sample{Seconds: float64(i * 5), Devices: []thermal.Device{{ID: "demo:gpu", Name: "Demo GPU (synthetic)", Kind: "gpu", Source: "demo", Temp: thermal.Number(temp), Util: thermal.Number(100), Power: thermal.Number(power), Clock: thermal.Number(clock), Throttled: &throttle}}})
		}
	}
	return a, b
}
