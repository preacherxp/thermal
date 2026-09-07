//go:build !windows

package thermal

import "errors"

func windowsModel() (string, string, string, error) { return "", "", "", errors.New("not Windows") }
func windowsGPUs() []GPUInfo                        { return nil }
func nativeNVIDIA() ([]Device, error) {
	return nil, errors.New("native NVIDIA driver API is currently supported on Windows; Linux can use kernel hwmon or opt-in nvidia-smi")
}
