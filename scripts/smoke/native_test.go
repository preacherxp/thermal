package smoke

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"thermal-cli/internal/thermal"
)

func native(t *testing.T, args ...string) ([]byte, string, error) {
	t.Helper()
	if os.Getenv("THERMAL_TEST_NATIVE") != "1" {
		t.Skip("build local binary and set THERMAL_TEST_NATIVE=1")
	}
	name := "thermal"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "dist", runtime.GOOS+"-"+runtime.GOARCH, name))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	for _, env := range os.Environ() {
		key := strings.SplitN(env, "=", 2)[0]
		if !strings.EqualFold(key, "PATH") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "PATH=", "THERMAL_EXTERNAL_PROVIDERS=0", "THERMAL_NVIDIA_SMI=helper-must-not-run.exe")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return out, stderr.String(), err
}

func TestVersionAndExitStatus(t *testing.T) {
	out, stderr, err := native(t, "--version")
	if err != nil || strings.TrimSpace(string(out)) != "thermal "+thermal.Version {
		t.Fatalf("version: %s %s %v", out, stderr, err)
	}
	_, stderr, err = native(t, "unrecognized command")
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(stderr, `unknown command "unrecognized command"`) {
		t.Fatalf("exit/argument preservation: %s %v", stderr, err)
	}
}

func TestDoctorAndSurveyWithoutHelpers(t *testing.T) {
	for _, command := range []string{"doctor", "survey"} {
		out, stderr, err := native(t, command, "--json")
		if err != nil {
			t.Fatalf("%s: %s %v", command, stderr, err)
		}
		var result map[string]any
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatal(err)
		}
		if command == "doctor" {
			if result["os"] != runtime.GOOS {
				t.Fatalf("unexpected OS: %v", result["os"])
			}
			sample := result["sample"].(map[string]any)
			for _, device := range sample["devices"].([]any) {
				source := device.(map[string]any)["source"]
				if source == "nvidia-smi" || source == "macmon" || source == "LibreHardwareMonitor" {
					t.Fatalf("external provider: %s", source)
				}
			}
		} else if result["hardware"].(map[string]any)["logical_cpus"].(float64) <= 0 {
			t.Fatal("missing CPU count")
		}
	}
}

func TestRecordingAndPNGWithoutHelpers(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "run with spaces.json")
	out, stderr, err := native(t, "record", "--survey", "skip", "--duration", "1s", "--out", output, "--json")
	if err != nil {
		t.Fatalf("record: %s %v", stderr, err)
	}
	var run, saved map[string]any
	if err := json.Unmarshal(out, &run); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(run, saved) || run["status"] != "complete" || run["requested_seconds"] != float64(1) || run["operations"] != nil {
		t.Fatalf("unexpected recording: %s", out)
	}
	png, err := os.ReadFile(strings.TrimSuffix(output, ".json") + ".png")
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 24 || !bytes.Equal(png[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) || binary.BigEndian.Uint32(png[16:20]) != 1440 {
		t.Fatal("invalid PNG")
	}
	output = filepath.Join(dir, "no image.json")
	out, stderr, err = native(t, "record", "--survey", "skip", "--duration", "1s", "--out", output, "--json", "--no-png")
	if err != nil || !json.Valid(out) {
		t.Fatalf("JSON only: %s %s %v", out, stderr, err)
	}
	if _, err := os.Stat(strings.TrimSuffix(output, ".json") + ".png"); !os.IsNotExist(err) {
		t.Fatal("--no-png created an image")
	}
}
