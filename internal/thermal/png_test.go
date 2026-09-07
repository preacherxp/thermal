package thermal

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPNGReports(t *testing.T) {
	a := fixture("baseline", 60, 20, 1000)
	b := fixture("fans-cleaned", 55, 25, 1200)
	for _, tt := range []struct {
		name string
		a    Run
		b    *Run
	}{
		{"single", a, nil}, {"comparison", a, &b}, {"empty", Run{Status: "refused"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteReportPNG(&out, tt.a, tt.b); err != nil {
				t.Fatal(err)
			}
			im, err := png.Decode(&out)
			if err != nil {
				t.Fatal(err)
			}
			if im.Bounds().Dx() != 1440 || im.Bounds().Dy() < 800 {
				t.Fatal(im.Bounds())
			}
			if im.At(50, 35) != ink {
				t.Fatal("header missing")
			}
		})
	}
}

func TestPNGDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.png")
	r := fixture("baseline", 60, 20, 1000)
	if err := SaveReportPNG(path, r, nil); err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(path)
	if err := SaveReportPNG(path, r, nil); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected exists: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, want) {
		t.Fatal("overwrote PNG")
	}
	if err := SaveReportPNG(filepath.Join(t.TempDir(), "run.json"), r, nil); err == nil {
		t.Fatal("accepted non-PNG path")
	}
}

type brokenPNGWriter struct{}

func (brokenPNGWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestPNGWriterFailure(t *testing.T) {
	if err := WriteReportPNG(brokenPNGWriter{}, fixture("baseline", 60, 20, 1000), nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}

func TestPNGChartPreservesMissingReadings(t *testing.T) {
	if err := loadReportFont(); err != nil {
		t.Fatal(err)
	}
	r := Run{Samples: []Sample{
		{Seconds: 0, Devices: []Device{{ID: "gpu", Temp: Number(60)}}},
		{Seconds: 5, Devices: []Device{{ID: "gpu"}}},
		{Seconds: 10, Devices: []Device{{ID: "gpu", Temp: Number(60)}}},
	}}
	im := image.NewRGBA(image.Rect(0, 0, 1440, 400))
	rect(im, 0, 0, 1440, 400, white)
	temperatureChart(im, 50, "gpu", r, nil)
	if im.RGBAAt(737, 145) == teal {
		t.Fatal("line crossed a missing sample")
	}
	r.Samples[1].Devices[0].Temp = Number(60)
	temperatureChart(im, 50, "gpu", r, nil)
	if im.RGBAAt(737, 145) != teal {
		t.Fatal("known sample was not plotted")
	}
}

func TestPNGWrapLongText(t *testing.T) {
	if err := loadReportFont(); err != nil {
		t.Fatal(err)
	}
	for _, line := range wrapPNG(strings.Repeat("verylongname", 80)+" unknown 日本語", 24, 280) {
		if textWidth(line, 24) > 280 {
			t.Fatalf("text exceeds card: %s", line)
		}
	}
}

func TestPNGManySensorsAndSuites(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	for i := range r.Samples {
		r.Samples[i].Devices = nil
		for j := 0; j < 48; j++ {
			r.Samples[i].Devices = append(r.Samples[i].Devices, Device{ID: fmt.Sprint(j), Name: fmt.Sprintf("Core %d", j), Kind: "cpu", Temp: Number(60)})
		}
	}
	gpu := r
	gpu.Workload = "gpu-integer-v1"
	suite := r
	suite.Workload = "cpu-gpu-suite-v1"
	suite.Samples = nil
	suite.Phases = []Run{r, gpu}
	for _, run := range []Run{r, suite} {
		for _, after := range []*Run{nil, &run} {
			var out bytes.Buffer
			if err := WriteReportPNG(&out, run, after); err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(&out)
			if err != nil || cfg.Height > 24000 {
				t.Fatalf("invalid PNG: %+v %v", cfg, err)
			}
		}
	}
}
