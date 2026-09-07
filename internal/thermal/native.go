package thermal

import (
	"context"
	"os"
	"sort"
	"time"
)

func externalProviders() bool { return os.Getenv("THERMAL_EXTERNAL_PROVIDERS") == "1" }

type cpuSampler struct {
	total, idle uint64
	primed      bool
}

func (s *cpuSampler) update(total, idle uint64) *float64 {
	oldTotal, oldIdle, ready := s.total, s.idle, s.primed
	s.total, s.idle, s.primed = total, idle, true
	if !ready || total <= oldTotal || idle < oldIdle || idle-oldIdle > total-oldTotal {
		return nil
	}
	return Number(100 * float64((total-oldTotal)-(idle-oldIdle)) / float64(total-oldTotal))
}

type processSample struct {
	pid     int32
	name    string
	seconds float64
	start   string
	at      time.Time
}

func TopApps(ctx context.Context) []App {
	before := nativeProcesses(ctx)
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(400 * time.Millisecond):
	}
	after := nativeProcesses(ctx)
	apps := []App{}
	for pid, v := range after {
		old, ok := before[pid]
		if !ok || pid == 0 || pid == int32(os.Getpid()) || old.start != v.start {
			continue
		}
		elapsed := v.at.Sub(old.at).Seconds()
		if elapsed <= 0 || v.seconds < old.seconds {
			continue
		}
		percent := (v.seconds - old.seconds) / elapsed * 100
		if percent > 0.1 {
			apps = append(apps, App{pid, v.name, percent})
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].CPU > apps[j].CPU })
	if len(apps) > 5 {
		apps = apps[:5]
	}
	return apps
}
