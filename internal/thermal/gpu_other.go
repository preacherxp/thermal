//go:build !windows

package thermal

import "fmt"

func newGPUBackend() (gpuBackend, error) {
	return nil, fmt.Errorf("built-in GPU compute benchmark currently requires Windows and an OpenCL GPU driver; use record with an external workload on this platform")
}
