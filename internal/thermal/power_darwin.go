package thermal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Private IOReport ABI: https://github.com/vladkens/macmon/blob/main/src_lib/sources.rs.
// All arguments and return values use the existing integer/pointer C bridge.
type ioReportAPI struct {
	procs                                   map[string]uintptr
	channels, subscribed, subscription, key uintptr
	mu                                      sync.Mutex
}

var nativeIOReport = sync.OnceValues(func() (_ *ioReportAPI, err error) {
	chip, _ := syscall.Sysctl("machdep.cpu.brand_string")
	if !strings.HasPrefix(chip, "Apple M") {
		return nil, fmt.Errorf("IOReport energy monitoring requires Apple Silicon")
	}
	handle, procs, err := macOpen("/usr/lib/libIOReport.dylib", []string{
		"IOReportCopyChannelsInGroup", "IOReportCreateSubscription", "IOReportCreateSamples",
		"IOReportChannelGetChannelName", "IOReportChannelGetUnitLabel", "IOReportChannelGetFormat", "IOReportSimpleGetIntegerValue",
		"CFStringCreateWithCString", "CFStringGetCString", "CFDictionaryCreateMutableCopy", "CFDictionaryGetValue",
		"CFArrayGetCount", "CFArrayGetValueAtIndex", "CFRelease", "CFGetTypeID", "CFArrayGetTypeID", "CFDictionaryGetTypeID", "CFStringGetTypeID",
	})
	if err != nil {
		return nil, err
	}
	api := &ioReportAPI{procs: procs}
	defer func() {
		if err != nil {
			for _, h := range []uintptr{api.subscription, api.subscribed, api.channels, api.key} {
				if h != 0 {
					macCall(procs["CFRelease"], h)
				}
			}
			macCall(macDlclose, handle)
		}
	}()
	group := api.stringRef("Energy Model")
	if group == 0 {
		return nil, fmt.Errorf("IOReport group allocation failed")
	}
	defer macCall(procs["CFRelease"], group)
	original := macCall(procs["IOReportCopyChannelsInGroup"], group, 0, 0, 0, 0)
	if original == 0 {
		return nil, fmt.Errorf("IOReport Energy Model unavailable")
	}
	defer macCall(procs["CFRelease"], original)
	if !api.isType(original, "CFDictionaryGetTypeID") {
		return nil, fmt.Errorf("invalid IOReport channel dictionary")
	}
	api.channels = macCall(procs["CFDictionaryCreateMutableCopy"], 0, 0, original)
	api.key = api.stringRef("IOReportChannels")
	if api.channels == 0 || api.key == 0 {
		return nil, fmt.Errorf("IOReport channel allocation failed")
	}
	api.subscription = macCall(procs["IOReportCreateSubscription"], 0, api.channels, uintptr(unsafe.Pointer(&api.subscribed)), 0, 0)
	if api.subscription == 0 || api.subscribed == 0 {
		return nil, fmt.Errorf("IOReport subscription unavailable")
	}
	// ponytail: one fixed subscription for the process lifetime; per-reader Go
	// baselines avoid retaining native samples or requiring a new Close API.
	return api, nil
})

func (a *ioReportAPI) stringRef(value string) uintptr {
	p, err := syscall.BytePtrFromString(value)
	if err != nil {
		return 0
	}
	return macCall(a.procs["CFStringCreateWithCString"], 0, uintptr(unsafe.Pointer(p)), 0x08000100)
}
func (a *ioReportAPI) isType(value uintptr, typeFunc string) bool {
	return value != 0 && macCall(a.procs["CFGetTypeID"], value) == macCall(a.procs[typeFunc])
}
func (a *ioReportAPI) stringValue(value uintptr) string {
	if !a.isType(value, "CFStringGetTypeID") {
		return ""
	}
	var buf [256]byte
	if macCall(a.procs["CFStringGetCString"], value, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0x08000100) == 0 {
		return ""
	}
	return strings.TrimRight(string(buf[:]), "\x00")
}

func (r *ioReportSampler) read(ctx context.Context) (_ map[string][]PowerReading, err error) {
	defer func() {
		if err != nil {
			r.previous = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	api, err := nativeIOReport()
	if err != nil {
		return nil, err
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sample := macCall(api.procs["IOReportCreateSamples"], api.subscription, api.subscribed, 0)
	if sample == 0 {
		return nil, fmt.Errorf("IOReport sample unavailable")
	}
	defer macCall(api.procs["CFRelease"], sample)
	now := time.Now()
	if !api.isType(sample, "CFDictionaryGetTypeID") {
		return nil, fmt.Errorf("invalid IOReport sample")
	}
	array := macCall(api.procs["CFDictionaryGetValue"], sample, api.key)
	if !api.isType(array, "CFArrayGetTypeID") {
		return nil, fmt.Errorf("IOReport channels unavailable")
	}
	count := macCall(api.procs["CFArrayGetCount"], array)
	if count > 65536 {
		return nil, fmt.Errorf("invalid IOReport channel count")
	}
	var channels []ioEnergy
	seen := map[string]bool{}
	for i := uintptr(0); i < count; i++ {
		entry := macCall(api.procs["CFArrayGetValueAtIndex"], array, i)
		if !api.isType(entry, "CFDictionaryGetTypeID") {
			continue
		}
		name := api.stringValue(macCall(api.procs["IOReportChannelGetChannelName"], entry))
		if ioEnergyKind(name) == "" {
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("ambiguous IOReport channel: %s", name)
		}
		seen[name] = true
		unit := api.stringValue(macCall(api.procs["IOReportChannelGetUnitLabel"], entry))
		value := int64(-1)
		if macCall(api.procs["IOReportChannelGetFormat"], entry) == 1 { // kIOReportFormatSimple
			value = int64(macCall(api.procs["IOReportSimpleGetIntegerValue"], entry, 0))
		}
		channels = append(channels, ioEnergy{name, unit, value})
	}
	readings := r.update(channels, now)
	if len(readings) == 0 {
		return nil, fmt.Errorf("IOReport CPU/GPU energy channels unavailable")
	}
	return readings, nil
}
