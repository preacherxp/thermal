package thermal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	Preparation                                  *Survey
	Stage, Profile, PowerSource, Notes, Workload string
	Ambient                                      *float64
	Duration, Interval                           time.Duration
	Benchmark                                    bool
	Workers                                      int
	StopTemp                                     float64
	AllowUnmonitored                             bool
}

func (o Options) Validate() error {
	if !ValidStage(o.Stage) {
		return fmt.Errorf("unknown stage %q", o.Stage)
	}
	if o.Duration < time.Second || o.Duration > 30*time.Minute {
		return errors.New("duration must be between 1s and 30m")
	}
	if o.Interval < 200*time.Millisecond || o.Interval > 30*time.Second {
		return errors.New("interval must be between 200ms and 30s")
	}
	if o.Workers < 1 || o.Workers > runtime.NumCPU() {
		return fmt.Errorf("workers must be between 1 and %d", runtime.NumCPU())
	}
	if o.StopTemp < 50 || o.StopTemp > 100 {
		return errors.New("stop temperature must be between 50 and 100 C")
	}
	if o.Ambient != nil && (*o.Ambient < -10 || *o.Ambient > 50) {
		return errors.New("ambient temperature must be between -10 and 50 C")
	}
	return nil
}
func Guard(s Sample, stop float64, allowMissing bool) string {
	found := false
	for _, d := range s.Devices {
		if d.Kind != "cpu" || d.Temp == nil {
			continue
		}
		found = true
		limit := stop
		if d.Critical != nil && *d.Critical > 0 && *d.Critical-5 < limit {
			limit = *d.Critical - 5
		}
		if *d.Temp >= limit {
			return fmt.Sprintf("temperature limit: %s %.1f C >= %.1f C", d.Name, *d.Temp, limit)
		}
	}
	if !found && !allowMissing {
		return "CPU temperature unavailable; use doctor to configure sensors or explicitly pass --allow-unmonitored"
	}
	return ""
}

// Fixed SHA-256 workload, versioned in run metadata. The counter is throughput,
// not a generic CPU score. Every worker is joined before Capture returns.
func burn(ctx context.Context, operations *atomic.Uint64, wg *sync.WaitGroup) {
	defer wg.Done()
	data := [4096]byte{}
	var n uint64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		for i := 0; i < 128; i++ {
			binary.LittleEndian.PutUint64(data[:8], n)
			hash := sha256.Sum256(data[:])
			copy(data[8:40], hash[:])
			n++
		}
		operations.Add(128)
	}
}
func Capture(ctx context.Context, reader Reader, o Options, progress func(Sample)) (Run, error) {
	if err := o.Validate(); err != nil {
		return Run{}, err
	}
	r := NewRun()
	r.Preparation = o.Preparation
	r.Stage = o.Stage
	r.Profile = o.Profile
	r.PowerSource = o.PowerSource
	r.Notes = o.Notes
	r.Ambient = o.Ambient
	r.Workload = o.Workload
	r.Duration = o.Duration.Seconds()
	r.Interval = o.Interval.Seconds()
	if o.Benchmark {
		r.Workload = "cpu-sha256-v1"
		r.Workers = o.Workers
		r.StopTemp = o.StopTemp
	}
	r.Apps = TopApps(ctx)
	// Prime global CPU counters so the first saved utilization is an interval.
	reader.Read(ctx)
	s, warnings := reader.Read(ctx)
	s.Seconds = 0
	r.Warnings = warnings
	r.Samples = append(r.Samples, s)
	if progress != nil {
		progress(s)
	}
	if o.Benchmark {
		if reason := Guard(s, o.StopTemp, o.AllowUnmonitored); reason != "" {
			r.Status = "refused"
			r.Warnings = append(r.Warnings, reason)
			return r, nil
		}
		if o.AllowUnmonitored {
			r.Warnings = append(r.Warnings, "Unmonitored mode enabled: CPU sensor loss will not stop this benchmark")
		}
	}
	workCtx, cancel := context.WithTimeout(ctx, o.Duration)
	defer cancel()
	var wg sync.WaitGroup
	var operations atomic.Uint64
	start := time.Now()
	if o.Benchmark {
		for i := 0; i < o.Workers; i++ {
			wg.Add(1)
			go burn(workCtx, &operations, &wg)
		}
	}
	defer func() { cancel(); wg.Wait() }()
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-workCtx.Done():
			break loop
		case <-ticker.C:
			sample, w := reader.Read(workCtx)
			// A query cancelled at the duration boundary is not a sensor failure.
			if workCtx.Err() != nil {
				break loop
			}
			sample.Seconds = time.Since(start).Seconds()
			r.Samples = append(r.Samples, sample)
			for _, v := range w {
				if !slices.Contains(r.Warnings, v) {
					r.Warnings = append(r.Warnings, v)
				}
			}
			if progress != nil {
				progress(sample)
			}
			if o.Benchmark {
				if reason := Guard(sample, o.StopTemp, o.AllowUnmonitored); reason != "" {
					r.Status = "stopped"
					r.Warnings = append(r.Warnings, reason)
					cancel()
					break loop
				}
			}
		}
	}
	cancel()
	wg.Wait()
	r.Elapsed = time.Since(start).Seconds()
	r.Operations = operations.Load()
	if ctx.Err() != nil {
		r.Status = "interrupted"
	}
	if len(r.Samples) < 3 {
		r.Warnings = append(r.Warnings, "Too few samples for a sustained thermal assessment; run for longer")
	}
	return r, nil
}
