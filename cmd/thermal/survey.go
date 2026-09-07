package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"thermal-cli/internal/thermal"
)

type surveyFlags struct{ mode, tgp, cpu, gpu, benchmark, score, source *string }

func addSurveyFlags(f *flag.FlagSet) surveyFlags {
	return surveyFlags{
		f.String("survey", "auto", "Pre-run survey: auto (prompt on terminal), ask, or skip"),
		f.String("gpu-watts", "", "User-specified rated maximum watts for first discrete GPU"),
		f.String("cpu-material", "", "Current CPU material: unknown, factory, paste, liquid-metal, phase-change, pad, other"),
		f.String("gpu-material", "", "Current material for first discrete GPU; same choices as CPU"),
		f.String("expected-benchmark", "", "Name/version/preset of an additional benchmark target"),
		f.String("expected-score", "", "Expected score for --expected-benchmark"),
		f.String("score-source", "", "Source URL or note for a user-provided score"),
	}
}
func (f surveyFlags) validate() error {
	if *f.mode != "auto" && *f.mode != "ask" && *f.mode != "skip" {
		return errors.New("--survey must be auto, ask, or skip")
	}
	if (*f.benchmark == "") != (*f.score == "") {
		return errors.New("--expected-benchmark and --expected-score must be provided together")
	}
	if *f.tgp != "" {
		if _, e := thermal.PositiveNumber(*f.tgp, 2000); e != nil {
			return e
		}
	}
	if *f.score != "" {
		if _, e := thermal.PositiveNumber(*f.score, 1e15); e != nil {
			return e
		}
	}
	if *f.mode == "skip" && (*f.tgp != "" || *f.cpu != "" || *f.gpu != "" || *f.benchmark != "" || *f.source != "") {
		return errors.New("survey inputs cannot be combined with --survey skip")
	}
	return nil
}
func isTerminalInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	return thermal.TerminalFile(file)
}
func (f surveyFlags) prepare(ctx context.Context, profile string, in io.Reader, out io.Writer, jsonOutput bool) (*thermal.Survey, error) {
	if e := f.validate(); e != nil {
		return nil, e
	}
	if *f.mode == "skip" {
		return nil, nil
	}
	if *f.mode == "ask" && jsonOutput {
		return nil, errors.New("--survey ask cannot be combined with --json")
	}
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	hardware := thermal.DetectHardware(probe)
	cancel()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s := thermal.BuildSurvey(hardware, profile)
	if e := thermal.ApplySurveyInputs(&s, *f.tgp, *f.cpu, *f.gpu, *f.benchmark, *f.score, *f.source); e != nil {
		return nil, e
	}
	thermal.PrintSurvey(out, s)
	if *f.mode == "ask" || *f.mode == "auto" && !jsonOutput && isTerminalInput(in) {
		if e := thermal.PromptSurvey(ctx, in, out, &s); e != nil {
			return nil, e
		}
		fmt.Fprintln(out, "\n  Survey recorded. Profile and material answers describe your setup; no settings changed.")
	}
	return &s, nil
}
func surveyCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	f := flags("survey", errOut)
	profile := f.String("profile", "unknown", "Current thermal profile label")
	asJSON := f.Bool("json", false, "Output survey JSON without prompting")
	sf := addSurveyFlags(f)
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("survey does not accept positional arguments")
	}
	if *sf.mode == "skip" {
		return errors.New("standalone survey cannot be skipped")
	}
	human := out
	if *asJSON {
		human = io.Discard
	}
	s, e := sf.prepare(ctx, *profile, os.Stdin, human, *asJSON)
	if e != nil {
		return e
	}
	if *asJSON {
		return writeJSON(out, s)
	}
	return nil
}
