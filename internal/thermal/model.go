package thermal

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const Version = "0.4.0"

var Stages = []string{"baseline", "apps-closed", "profile-changed", "fans-cleaned", "repasted", "specialist"}

type Device struct {
	PowerReadings []PowerReading `json:"power_readings,omitempty"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Source        string         `json:"source"`
	Temp          *float64       `json:"temperature_c"`
	Util          *float64       `json:"utilization_pct,omitempty"`
	Power         *float64       `json:"power_w,omitempty"`
	Clock         *float64       `json:"clock_mhz,omitempty"`
	Critical      *float64       `json:"critical_c,omitempty"`
	Throttled     *bool          `json:"thermal_throttling,omitempty"`
}
type Sample struct {
	Seconds float64  `json:"seconds"`
	CPU     *float64 `json:"cpu_pct"`
	Devices []Device `json:"devices"`
}
type App struct {
	PID  int32   `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu_pct"`
}
type Run struct {
	Phases      []Run      `json:"phases,omitempty"`
	GPU         *GPUResult `json:"gpu_benchmark,omitempty"`
	Preparation *Survey    `json:"pre_run_survey,omitempty"`
	Schema      int        `json:"schema"`
	ID          string     `json:"id"`
	Created     time.Time  `json:"created"`
	Host        string     `json:"host"`
	OS          string     `json:"os"`
	Arch        string     `json:"arch"`
	CPUs        int        `json:"logical_cpus"`
	Stage       string     `json:"stage"`
	Profile     string     `json:"profile"`
	PowerSource string     `json:"power_source"`
	Ambient     *float64   `json:"ambient_c,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	Workload    string     `json:"workload"`
	Duration    float64    `json:"requested_seconds"`
	Interval    float64    `json:"interval_seconds"`
	Workers     int        `json:"workers,omitempty"`
	StopTemp    float64    `json:"stop_temperature_c,omitempty"`
	Status      string     `json:"status"`
	Operations  uint64     `json:"operations,omitempty"`
	Elapsed     float64    `json:"elapsed_seconds"`
	Samples     []Sample   `json:"samples"`
	Apps        []App      `json:"apps,omitempty"`
	Warnings    []string   `json:"warnings,omitempty"`
}

func Number(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
func ValidStage(s string) bool {
	for _, v := range Stages {
		if s == v {
			return true
		}
	}
	return false
}
func NewRun() Run {
	h, _ := os.Hostname()
	now := time.Now().UTC()
	return Run{Schema: 1, ID: now.Format("20060102T150405.000000000Z"), Created: now, Host: h, OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Status: "complete", Samples: []Sample{}}
}
func DataDir() string {
	if v := os.Getenv("THERMAL_DATA_DIR"); v != "" {
		return v
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return ".thermal"
	}
	return filepath.Join(d, "thermal", "runs")
}
func Save(r Run, path string) (string, error) {
	if path == "" {
		path = filepath.Join(DataDir(), r.ID+"-"+r.Stage+".json")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	// Exclusive creation keeps previous measurements intact.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(append(b, '\n'))
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return path, nil
}
func Load(path string) (Run, error) {
	var r Run
	f, err := os.Open(path)
	if err != nil {
		return r, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return r, err
	}
	if info.Size() > 32*1024*1024 {
		return r, errors.New("run file exceeds 32 MiB")
	}
	if err = json.NewDecoder(f).Decode(&r); err != nil {
		return r, err
	}
	return r, validateRun(r, false)
}

func validateRun(r Run, phase bool) error {
	if r.Schema != 1 || !ValidStage(r.Stage) {
		return errors.New("invalid run: expected schema 1 and a known stage")
	}
	if len(r.Phases) > 0 {
		if phase || r.Workload != "cpu-gpu-suite-v1" || len(r.Phases) != 2 || len(r.Samples) != 0 {
			return errors.New("invalid benchmark suite")
		}
		for i, p := range r.Phases {
			if p.Workload != []string{"cpu-sha256-v1", "gpu-integer-v1"}[i] {
				return errors.New("invalid benchmark phase order")
			}
			if err := validateRun(p, true); err != nil {
				return err
			}
		}
		return nil
	}
	if len(r.Samples) == 0 && !(phase && r.Status == "skipped") {
		return errors.New("invalid run: expected samples")
	}
	last := -1.0
	for _, s := range r.Samples {
		if s.Seconds < 0 || s.Seconds < last {
			return fmt.Errorf("invalid sample time %.2f", s.Seconds)
		}
		last = s.Seconds
		seen := map[string]bool{}
		for _, d := range s.Devices {
			if d.ID == "" || seen[d.ID] {
				return errors.New("missing or duplicate device ID")
			}
			seen[d.ID] = true
			if d.Temp != nil && (*d.Temp < -30 || *d.Temp > 150) {
				return errors.New("invalid temperature")
			}
		}
	}
	return nil
}
