package thermal

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

func benchmarkRun(o Options, workload string) Run {
	r := NewRun()
	r.Stage, r.Profile, r.PowerSource = o.Stage, o.Profile, o.PowerSource
	r.Preparation, r.Ambient, r.Notes = o.Preparation, o.Ambient, o.Notes
	r.Workload, r.Duration, r.Interval = workload, o.Duration.Seconds(), o.Interval.Seconds()
	r.StopTemp = o.StopTemp
	return r
}

// GPU sensor matching is deliberately exact. A reading from an unrelated GPU
// must not be presented as monitoring the selected compute device.
func gpuSensorID(s Sample, name string) string {
	id, count := "", 0
	for _, d := range s.Devices {
		if d.Kind == "gpu" && strings.EqualFold(strings.TrimSpace(d.Name), strings.TrimSpace(name)) {
			id = d.ID
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return id
}
func gpuGuard(s Sample, id string, stop float64, allowMissing bool) string {
	for _, d := range s.Devices {
		if id == "" || d.ID != id || d.Kind != "gpu" || d.Temp == nil {
			continue
		}
		limit := stop
		if d.Critical != nil && *d.Critical > 0 && *d.Critical-5 < limit {
			limit = *d.Critical - 5
		}
		if *d.Temp >= limit {
			return fmt.Sprintf("GPU temperature limit: %s %.1f C >= %.1f C", d.Name, *d.Temp, limit)
		}
		return ""
	}
	if !allowMissing {
		return "Temperature unavailable for the selected GPU; configure sensors or explicitly pass --allow-unmonitored"
	}
	return ""
}

func CaptureGPU(ctx context.Context, reader Reader, o Options, progress func(Sample)) (Run, error) {
	return captureGPU(ctx, reader, o, progress, startGPUProcess)
}

func captureGPU(ctx context.Context, reader Reader, o Options, progress func(Sample), factory func(context.Context, time.Duration) (gpuSession, error)) (Run, error) {
	if err := o.Validate(); err != nil {
		return Run{}, err
	}
	r := benchmarkRun(o, "gpu-integer-v1")
	r.Apps = TopApps(ctx)
	reader.Read(ctx)
	s, warnings := reader.Read(ctx)
	s.Seconds = 0
	r.Samples, r.Warnings = []Sample{s}, warnings
	if progress != nil {
		progress(s)
	}
	if ctx.Err() != nil {
		r.Status = "interrupted"
		return r, nil
	}
	gpu, err := factory(ctx, o.Duration)
	if err != nil {
		r.Status = "unavailable"
		if ctx.Err() != nil {
			r.Status = "interrupted"
		}
		r.Warnings = append(r.Warnings, err.Error())
		return r, nil
	}
	defer gpu.Close()
	result, _, _ := gpu.Result()
	r.GPU = &result
	// Driver setup may take seconds; check fresh telemetry before starting load.
	s, warnings = reader.Read(ctx)
	s.Seconds = 0
	r.Samples[0] = s
	for _, warning := range warnings {
		if !slices.Contains(r.Warnings, warning) {
			r.Warnings = append(r.Warnings, warning)
		}
	}
	if ctx.Err() != nil {
		r.Status = "interrupted"
		return r, nil
	}
	id := gpuSensorID(s, result.Device)
	s.TargetID = id
	r.Samples[0] = s
	if progress != nil {
		progress(s)
	}
	if reason := gpuGuard(s, id, o.StopTemp, o.AllowUnmonitored); reason != "" {
		r.Status = "refused"
		r.Warnings = append(r.Warnings, reason)
		return r, nil
	}
	if o.AllowUnmonitored {
		r.Warnings = append(r.Warnings, "Unmonitored mode enabled: GPU sensor loss will not stop this benchmark")
	}
	if err := gpu.Start(); err != nil {
		r.Status = "failed"
		r.Warnings = append(r.Warnings, err.Error())
		return r, nil
	}
	start := time.Now()
	workCtx, cancel := context.WithTimeout(ctx, o.Duration)
	defer cancel()
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	// Detect a driver/worker failure promptly even with a long sensor interval.
	health := time.NewTicker(100 * time.Millisecond)
	defer health.Stop()
loop:
	for {
		select {
		case <-workCtx.Done():
			break loop
		case <-health.C:
			_, err, done := gpu.Result()
			if err != nil {
				r.Status = "failed"
				r.Warnings = append(r.Warnings, err.Error())
				break loop
			}
			if done {
				break loop
			}
		case <-ticker.C:
			sample, w := reader.Read(workCtx)
			if workCtx.Err() != nil {
				break loop
			}
			sample.Seconds = time.Since(start).Seconds()
			sample.TargetID = id
			r.Samples = append(r.Samples, sample)
			for _, warning := range w {
				if !slices.Contains(r.Warnings, warning) {
					r.Warnings = append(r.Warnings, warning)
				}
			}
			if progress != nil {
				progress(sample)
			}
			if reason := gpuGuard(sample, id, o.StopTemp, o.AllowUnmonitored); reason != "" {
				r.Status = "stopped"
				r.Warnings = append(r.Warnings, reason)
				break loop
			}
		}
	}
	gpu.Close()
	r.Elapsed = time.Since(start).Seconds()
	result, err, done := gpu.Result()
	r.GPU = &result
	if ctx.Err() != nil {
		r.Status = "interrupted"
	}
	if err != nil && !slices.Contains(r.Warnings, err.Error()) {
		r.Warnings = append(r.Warnings, err.Error())
	}
	if r.Status == "complete" && (err != nil || !done || result.Rate() == nil) {
		r.Status = "failed"
		r.Warnings = append(r.Warnings, "GPU worker did not return a completed, verified result")
	}
	if len(r.Samples) < 3 {
		r.Warnings = append(r.Warnings, "Too few samples for a sustained thermal assessment; run for longer")
	}
	return r, nil
}

func CaptureBenchmarks(ctx context.Context, reader Reader, o Options, target string, progress func(string, Sample)) (Run, error) {
	return captureBenchmarks(ctx, reader, o, target, progress, Capture, CaptureGPU)
}

type captureFunc func(context.Context, Reader, Options, func(Sample)) (Run, error)

func captureBenchmarks(ctx context.Context, reader Reader, o Options, target string, progress func(string, Sample), cpuCapture, gpuCapture captureFunc) (Run, error) {
	if err := o.Validate(); err != nil {
		return Run{}, err
	}
	if target != "cpu" && target != "gpu" && target != "both" {
		return Run{}, fmt.Errorf("--target must be cpu, gpu, or both")
	}
	callback := func(kind string) func(Sample) {
		return func(s Sample) {
			if progress != nil {
				progress(kind, s)
			}
		}
	}
	o.Benchmark = true
	if target == "cpu" {
		return cpuCapture(ctx, reader, o, callback("CPU"))
	}
	if target == "gpu" {
		return gpuCapture(ctx, reader, o, callback("GPU"))
	}
	r := benchmarkRun(o, "cpu-gpu-suite-v1")
	r.Duration *= 2
	cpu, err := cpuCapture(ctx, reader, o, callback("CPU"))
	if err != nil {
		return Run{}, err
	}
	r.Phases = append(r.Phases, cpu)
	// An absent CPU sensor does not prevent a separately monitored GPU test.
	// A temperature stop or cancellation must not start another load.
	skip := ctx.Err() != nil || cpu.Status == "stopped" || strings.Contains(strings.Join(cpu.Warnings, " "), "temperature limit")
	var gpu Run
	if skip {
		gpu = benchmarkRun(o, "gpu-integer-v1")
		gpu.Status = "skipped"
		gpu.Warnings = []string{"GPU test skipped after CPU temperature/monitoring stop or cancellation"}
	} else {
		gpu, err = gpuCapture(ctx, reader, o, callback("GPU"))
		if err != nil {
			return Run{}, err
		}
	}
	r.Phases = append(r.Phases, gpu)
	r.Elapsed = cpu.Elapsed + gpu.Elapsed
	for _, phase := range r.Phases {
		if phase.Status != "complete" {
			r.Status = "partial"
			r.Warnings = append(r.Warnings, phase.Workload+": "+phase.Status+"; see phase warnings")
		}
	}
	if ctx.Err() != nil {
		r.Status = "interrupted"
	}
	return r, nil
}
