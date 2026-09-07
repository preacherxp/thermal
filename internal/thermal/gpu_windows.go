package thermal

// OpenCL 1.2 driver ABI: https://registry.khronos.org/OpenCL/specs/1.2/html/OpenCL_API.html
// Only GPU devices are selected; there is no CPU/software fallback.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const gpuItems = 262144
const gpuRounds = 1024
const gpuKernel = `
__kernel void thermal_integer(__global uint *output, uint seed) {
    uint id = get_global_id(0);
    uint x = id ^ seed;
    for (uint i = 0; i < THERMAL_ROUNDS; i++) {
        x ^= x >> 13;
        x *= 1664525u;
        x += 1013904223u;
        x ^= x << 7;
    }
    output[id] = x;
}`

type openCLGPU struct {
	dll                                             *syscall.DLL
	procs                                           map[string]*syscall.Proc
	device, context, queue, program, kernel, buffer uintptr
	name                                            string
	seed                                            uint32
}

//go:uintptrescapes
func (g *openCLGPU) call(name string, args ...uintptr) uintptr {
	r, _, _ := g.procs[name].Call(args...)
	return r
}
func clError(name string, code uintptr) error {
	if int32(code) != 0 {
		return fmt.Errorf("%s failed (OpenCL %d)", name, int32(code))
	}
	return nil
}
func (g *openCLGPU) info(device uintptr, key uint32) string {
	var b [1024]byte
	if g.call("clGetDeviceInfo", device, uintptr(key), uintptr(len(b)), uintptr(unsafe.Pointer(&b[0])), 0) != 0 {
		return ""
	}
	return strings.TrimRight(string(b[:]), "\x00")
}
func (g *openCLGPU) infoUint(device uintptr, key uint32) uint32 {
	var v uint32
	g.call("clGetDeviceInfo", device, uintptr(key), 4, uintptr(unsafe.Pointer(&v)), 0)
	return v
}
func newGPUBackend() (_ gpuBackend, err error) {
	root := os.Getenv("SystemRoot")
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("Windows system directory unavailable")
	}
	dll, err := syscall.LoadDLL(filepath.Join(root, "System32", "OpenCL.dll"))
	if err != nil {
		return nil, fmt.Errorf("OpenCL GPU driver unavailable: %w", err)
	}
	g := &openCLGPU{dll: dll, procs: map[string]*syscall.Proc{}}
	defer func() {
		if err != nil {
			g.Close()
		}
	}()
	for _, name := range []string{"clGetPlatformIDs", "clGetDeviceIDs", "clGetDeviceInfo", "clCreateContext", "clCreateCommandQueue", "clCreateProgramWithSource", "clBuildProgram", "clGetProgramBuildInfo", "clCreateKernel", "clCreateBuffer", "clSetKernelArg", "clEnqueueNDRangeKernel", "clEnqueueReadBuffer", "clReleaseMemObject", "clReleaseKernel", "clReleaseProgram", "clReleaseCommandQueue", "clReleaseContext"} {
		p, e := dll.FindProc(name)
		if e != nil {
			return nil, e
		}
		g.procs[name] = p
	}
	var count uint32
	if e := clError("clGetPlatformIDs", g.call("clGetPlatformIDs", 0, 0, uintptr(unsafe.Pointer(&count)))); e != nil {
		return nil, e
	}
	if count == 0 || count > 64 {
		return nil, fmt.Errorf("no OpenCL GPU platforms available")
	}
	platforms := make([]uintptr, count)
	if e := clError("clGetPlatformIDs", g.call("clGetPlatformIDs", uintptr(count), uintptr(unsafe.Pointer(&platforms[0])), 0)); e != nil {
		return nil, e
	}
	bestScore := -1
	for _, platform := range platforms {
		var n uint32
		if g.call("clGetDeviceIDs", platform, 4, 0, 0, uintptr(unsafe.Pointer(&n))) != 0 || n == 0 || n > 64 {
			continue
		}
		devices := make([]uintptr, n)
		if g.call("clGetDeviceIDs", platform, 4, uintptr(n), uintptr(unsafe.Pointer(&devices[0])), 0) != 0 {
			continue
		}
		for _, d := range devices {
			if g.infoUint(d, 0x1027) == 0 || g.infoUint(d, 0x1028) == 0 {
				continue
			} // available + compiler
			score := int(g.infoUint(d, 0x1002)) // compute units; prefer discrete memory
			if g.infoUint(d, 0x1035) == 0 {
				score += 100000
			}
			if score > bestScore {
				g.device = d
				bestScore = score
			}
		}
	}
	if g.device == 0 {
		return nil, fmt.Errorf("no OpenCL GPU with an available compiler; install the GPU manufacturer's graphics driver")
	}
	g.name = g.info(g.device, 0x102B)
	var status int32
	g.context = g.call("clCreateContext", 0, 1, uintptr(unsafe.Pointer(&g.device)), 0, 0, uintptr(unsafe.Pointer(&status)))
	if status != 0 || g.context == 0 {
		return nil, fmt.Errorf("OpenCL context failed (%d)", status)
	}
	g.queue = g.call("clCreateCommandQueue", g.context, g.device, 0, uintptr(unsafe.Pointer(&status)))
	if status != 0 || g.queue == 0 {
		return nil, fmt.Errorf("OpenCL queue failed (%d)", status)
	}
	source := []byte(fmt.Sprintf("#define THERMAL_ROUNDS %d\n%s", gpuRounds, gpuKernel))
	sourcePtr, sourceSize := &source[0], uintptr(len(source))
	g.program = g.call("clCreateProgramWithSource", g.context, 1, uintptr(unsafe.Pointer(&sourcePtr)), uintptr(unsafe.Pointer(&sourceSize)), uintptr(unsafe.Pointer(&status)))
	if status != 0 || g.program == 0 {
		return nil, fmt.Errorf("OpenCL program failed (%d)", status)
	}
	if r := g.call("clBuildProgram", g.program, 1, uintptr(unsafe.Pointer(&g.device)), 0, 0, 0); r != 0 {
		var log [4096]byte
		g.call("clGetProgramBuildInfo", g.program, g.device, 0x1183, uintptr(len(log)), uintptr(unsafe.Pointer(&log[0])), 0)
		return nil, fmt.Errorf("OpenCL kernel build failed (%d): %s", int32(r), strings.TrimRight(string(log[:]), "\x00"))
	}
	entry, _ := syscall.BytePtrFromString("thermal_integer")
	g.kernel = g.call("clCreateKernel", g.program, uintptr(unsafe.Pointer(entry)), uintptr(unsafe.Pointer(&status)))
	if status != 0 || g.kernel == 0 {
		return nil, fmt.Errorf("OpenCL kernel failed (%d)", status)
	}
	g.buffer = g.call("clCreateBuffer", g.context, 1, gpuItems*4, 0, uintptr(unsafe.Pointer(&status)))
	if status != 0 || g.buffer == 0 {
		return nil, fmt.Errorf("OpenCL buffer failed (%d)", status)
	}
	if err := clError("clSetKernelArg", g.call("clSetKernelArg", g.kernel, 0, unsafe.Sizeof(g.buffer), uintptr(unsafe.Pointer(&g.buffer)))); err != nil {
		return nil, err
	}
	return g, nil
}

func gpuExpected(id, seed uint32) uint32 {
	x := id ^ seed
	for i := 0; i < gpuRounds; i++ {
		x ^= x >> 13
		x *= 1664525
		x += 1013904223
		x ^= x << 7
	}
	return x
}
func (g *openCLGPU) Name() string { return g.name }
func (g *openCLGPU) Step() (uint64, error) {
	g.seed++
	if err := clError("clSetKernelArg", g.call("clSetKernelArg", g.kernel, 1, 4, uintptr(unsafe.Pointer(&g.seed)))); err != nil {
		return 0, err
	}
	n := uintptr(gpuItems)
	if err := clError("clEnqueueNDRangeKernel", g.call("clEnqueueNDRangeKernel", g.queue, g.kernel, 1, 0, uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0)); err != nil {
		return 0, err
	}
	// Blocking read verifies completed device work and prevents an unbounded queue.
	// Rotate the checked range so the benchmark does not only verify item zero.
	var values [8]uint32
	first := (g.seed * 997) % (gpuItems - uint32(len(values)))
	if err := clError("clEnqueueReadBuffer", g.call("clEnqueueReadBuffer", g.queue, g.buffer, 1, uintptr(first)*4, unsafe.Sizeof(values), uintptr(unsafe.Pointer(&values[0])), 0, 0, 0)); err != nil {
		return 0, err
	}
	for i, value := range values {
		if value != gpuExpected(first+uint32(i), g.seed) {
			return 0, fmt.Errorf("GPU result verification failed at item %d", first+uint32(i))
		}
	}
	return gpuItems * gpuRounds, nil
}
func (g *openCLGPU) Close() {
	for _, item := range []struct {
		name   string
		handle uintptr
	}{{"clReleaseMemObject", g.buffer}, {"clReleaseKernel", g.kernel}, {"clReleaseProgram", g.program}, {"clReleaseCommandQueue", g.queue}, {"clReleaseContext", g.context}} {
		if item.handle != 0 && g.procs[item.name] != nil {
			g.call(item.name, item.handle)
		}
	}
	if g.dll != nil {
		g.dll.Release()
	}
}
