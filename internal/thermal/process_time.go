package thermal

import (
	"strconv"
	"strings"
)

func parseProcessTime(value string) (float64, bool) {
	days := float64(0)
	if a, b, ok := strings.Cut(value, "-"); ok {
		v, e := strconv.ParseUint(a, 10, 32)
		if e != nil {
			return 0, false
		}
		days = float64(v)
		value = b
	}
	f := strings.Split(value, ":")
	if len(f) < 2 || len(f) > 3 {
		return 0, false
	}
	total := float64(0)
	for _, part := range f {
		v, e := strconv.ParseFloat(part, 64)
		if e != nil || Number(v) == nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}
	return total + days*86400, true
}
