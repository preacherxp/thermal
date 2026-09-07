package thermal

import (
	"strconv"
	"strings"
	"time"
)

func parseCPUTicks(raw string) (uint64, uint64, bool) {
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var total, idle uint64
		for i := 1; i < len(f) && i <= 8; i++ {
			n, e := strconv.ParseUint(f[i], 10, 64)
			if e != nil {
				return 0, 0, false
			}
			total += n
			if i == 4 || i == 5 {
				idle += n
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}
func parseProcessStat(raw string, hz float64) (processSample, bool) {
	first, last := strings.Index(raw, "("), strings.LastIndex(raw, ")")
	if first < 1 || last <= first || hz <= 0 {
		return processSample{}, false
	}
	pid, e := strconv.ParseInt(strings.TrimSpace(raw[:first]), 10, 32)
	if e != nil {
		return processSample{}, false
	}
	f := strings.Fields(raw[last+1:])
	if len(f) < 20 {
		return processSample{}, false
	}
	u, e := strconv.ParseUint(f[11], 10, 64)
	if e != nil {
		return processSample{}, false
	}
	s, e := strconv.ParseUint(f[12], 10, 64)
	if e != nil {
		return processSample{}, false
	}
	return processSample{int32(pid), raw[first+1 : last], float64(u+s) / hz, f[19], time.Now()}, true
}

func parseMacCPU(raw string) *float64 {
	var result *float64
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "CPU usage:") {
			f := strings.Fields(line)
			if len(f) >= 6 {
				user := parseNumber(strings.TrimSuffix(f[2], "%"))
				system := parseNumber(strings.TrimSuffix(f[4], "%"))
				if user != nil && system != nil {
					result = Number(*user + *system)
				}
			}
		}
	}
	return result
}
