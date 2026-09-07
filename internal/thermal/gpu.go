package thermal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// GPUResult counts completed, verified kernel work, not graphics frames or FLOPS.
type GPUResult struct {
	Device     string  `json:"device"`
	Backend    string  `json:"backend"`
	Workload   string  `json:"workload"`
	Iterations uint64  `json:"iterations"`
	Elapsed    float64 `json:"elapsed_seconds"`
	Verified   bool    `json:"verified"`
}

func (g GPUResult) Rate() *float64 {
	if !g.Verified || g.Elapsed <= 0 || g.Iterations == 0 {
		return nil
	}
	return Number(float64(g.Iterations) / g.Elapsed)
}

type gpuBackend interface {
	Name() string
	Step() (uint64, error)
	Close()
}

type gpuMessage struct {
	Type   string    `json:"type"`
	Result GPUResult `json:"result"`
	Error  string    `json:"error,omitempty"`
}

// RunGPUWorker runs in a child process so a blocked driver cannot prevent the
// parent from stopping the workload and saving measurements.
func RunGPUWorker(ctx context.Context, duration time.Duration, in io.Reader, out io.Writer) error {
	if duration < time.Second || duration > 30*time.Minute {
		return fmt.Errorf("GPU duration must be between 1s and 30m")
	}
	b, err := newGPUBackend()
	if err != nil {
		return err
	}
	defer b.Close()
	result := GPUResult{Device: b.Name(), Backend: "OpenCL", Workload: "gpu-integer-v1"}
	enc := json.NewEncoder(out)
	if err := enc.Encode(gpuMessage{Type: "ready", Result: result}); err != nil {
		return err
	}
	input := bufio.NewScanner(in)
	if !input.Scan() || input.Text() != "start" {
		return nil
	}
	workCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	go func() { input.Scan(); cancel() }() // Stop command or parent closing its pipe.
	start, last := time.Now(), time.Now()
	for workCtx.Err() == nil {
		n, err := b.Step()
		if err != nil {
			_ = enc.Encode(gpuMessage{Type: "error", Result: result, Error: err.Error()})
			return err
		}
		result.Iterations += n
		result.Elapsed = time.Since(start).Seconds()
		result.Verified = true
		if time.Since(last) >= 250*time.Millisecond {
			if err := enc.Encode(gpuMessage{Type: "progress", Result: result}); err != nil {
				return err
			}
			last = time.Now()
		}
	}
	return enc.Encode(gpuMessage{Type: "done", Result: result})
}

type gpuSession interface {
	Result() (GPUResult, error, bool)
	Start() error
	Close()
}

type gpuProcess struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.Mutex
	result    GPUResult
	err       error
	finished  bool
	closeOnce sync.Once
}

func startGPUProcess(ctx context.Context, duration time.Duration) (gpuSession, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	p := &gpuProcess{cancel: cancel, done: make(chan struct{})}
	p.cmd = exec.CommandContext(childCtx, exe, "__gpu-worker", duration.String())
	configureCommand(p.cmd)
	p.cmd.WaitDelay = time.Second
	p.in, err = p.cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		p.in.Close()
		cancel()
		return nil, err
	}
	// Worker errors are structured on stdout; never mix them into the run's JSON.
	if err = p.cmd.Start(); err != nil {
		p.in.Close()
		stdout.Close()
		cancel()
		return nil, err
	}
	ready := make(chan struct{})
	go func() {
		defer close(p.done)
		dec := json.NewDecoder(stdout)
		announced := false
		for {
			var msg gpuMessage
			if err := dec.Decode(&msg); err != nil {
				if err != io.EOF {
					p.mu.Lock()
					p.err = err
					p.mu.Unlock()
				}
				break
			}
			p.mu.Lock()
			if msg.Result.Device != "" {
				p.result = msg.Result
			}
			if msg.Error != "" {
				p.err = fmt.Errorf("%s", msg.Error)
			}
			if msg.Type == "done" {
				p.finished = true
			}
			p.mu.Unlock()
			if msg.Type == "ready" && !announced {
				close(ready)
				announced = true
			}
		}
		err := p.cmd.Wait()
		p.mu.Lock()
		if p.err == nil && err != nil {
			p.err = fmt.Errorf("GPU worker: %w", err)
		}
		if p.err == nil && !p.finished {
			p.err = fmt.Errorf("GPU worker exited before completing")
		}
		p.mu.Unlock()
	}()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		return p, nil
	case <-p.done:
		_, err, _ := p.Result()
		p.Close()
		return nil, err
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	case <-timer.C:
		p.Close()
		return nil, fmt.Errorf("GPU driver initialization timed out")
	}
}

func (p *gpuProcess) Result() (GPUResult, error, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result, p.err, p.finished
}
func (p *gpuProcess) Start() error { _, err := io.WriteString(p.in, "start\n"); return err }
func (p *gpuProcess) Close() {
	p.closeOnce.Do(func() {
		p.in.Close()
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-p.done:
		case <-timer.C:
			p.cancel()
			<-p.done
		}
		p.cancel()
	})
}

func GPUWorkerError(out io.Writer, err error) {
	_ = json.NewEncoder(out).Encode(gpuMessage{Type: "error", Error: err.Error()})
}
