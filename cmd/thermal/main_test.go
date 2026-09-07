package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--help"}, 0, "THERMAL"},
		{[]string{"help"}, 0, "thermal [options]"},
		{[]string{"-h"}, 0, "without prompts"},
		{[]string{"--version"}, 0, "thermal 0.7.0"},
		{[]string{"--duration", "-1s"}, 1, "duration"},
		{[]string{"--stage", "wrong"}, 1, "unknown stage"},
		{[]string{"--unknown"}, 1, "flag provided but not defined"},
		{[]string{"--target", "invalid"}, 1, "--target"},
		{[]string{"record", "--target", "gpu"}, 1, "flag provided but not defined"},
		{[]string{"version"}, 0, "0.7.0"},
		{[]string{"demo"}, 0, "319.0 MHz"},
		{[]string{"benchmark", "--help"}, 0, "duration"},
		{[]string{"benchmark", "--duration", "-1s"}, 1, "duration"},
		{[]string{"benchmark", "--stop-temp", "NaN"}, 1, "invalid"},
		{[]string{"record", "--stage", "wrong"}, 1, "unknown stage"},
		{[]string{"compare"}, 1, "usage"},
		{[]string{"nope"}, 1, "unknown command"},
	} {
		var out, stderr bytes.Buffer
		code := run(tt.args, &out, &stderr)
		if code != tt.code || !strings.Contains(out.String()+stderr.String(), tt.want) {
			t.Fatalf("%v: code %d, out %s %s", tt.args, code, &out, &stderr)
		}
	}
}
func TestCSVImportAndReport(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "input.csv")
	json := filepath.Join(dir, "run.json")
	err := os.WriteFile(csv, []byte("sec,temp_C,power_W,util_pct,clock_MHz,pstate,hw_thermal,sw_thermal\n0,61,7,5,210,P8,Not Active,Not Active\n5,87,104,100,1882,P0,Not Active,Active\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if run([]string{"import", "--stage", "fans-cleaned", "--out", json, csv}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	if run([]string{"report", json}, &out, &stderr) != 0 || !strings.Contains(strings.ToLower(out.String()), "fans-cleaned") {
		t.Fatal(out.String(), stderr.String())
	}
	if run([]string{"import", "--out", json, csv}, &out, &stderr) != 1 {
		t.Fatal("import overwrote file")
	}
}

func TestSurveyFlags(t *testing.T) {
	for _, args := range [][]string{
		{"survey", "--help"},
		{"survey", "--survey", "invalid"},
		{"survey", "--expected-benchmark", "Time Spy"},
		{"benchmark", "--survey", "invalid"},
		{"benchmark", "--survey", "skip", "--gpu-watts", "175"},
	} {
		var out, stderr bytes.Buffer
		code := run(args, &out, &stderr)
		if args[1] == "--help" {
			if code != 0 {
				t.Fatal(stderr.String())
			}
		} else if code != 1 {
			t.Fatalf("%v accepted", args)
		}
	}
}

func TestRedirectedInputIsNotTerminal(t *testing.T) {
	f, e := os.Open(os.DevNull)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if isTerminalInput(f) {
		t.Fatal("null device must not trigger interactive survey")
	}
	if isTerminalInput(strings.NewReader("")) {
		t.Fatal("redirected input must not prompt automatically")
	}
}
