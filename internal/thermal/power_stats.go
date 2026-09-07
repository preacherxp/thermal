package thermal

type PowerStats struct {
	Name    string   `json:"name"`
	Mode    string   `json:"mode"`
	Current *float64 `json:"last_w"`
	Min     *float64 `json:"min_w"`
	Mean    *float64 `json:"mean_w"`
	Peak    *float64 `json:"peak_w"`
	Samples int      `json:"samples"`
}
type PowerDelta struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Mode      string     `json:"mode"`
	Before    PowerStats `json:"before"`
	After     PowerStats `json:"after"`
	MeanDelta *float64   `json:"mean_delta_w"`
	PeakDelta *float64   `json:"peak_delta_w"`
}

func SummarizePower(r Run, sustained bool) map[string]map[string]PowerStats {
	out := map[string]map[string]PowerStats{}
	values := map[string]map[string][]float64{}
	cutoff := 0.0
	if sustained && len(r.Samples) > 0 {
		cutoff = r.Samples[len(r.Samples)-1].Seconds * 0.75
	}
	for _, sample := range r.Samples {
		if sample.Seconds < cutoff {
			continue
		}
		for _, channels := range out {
			for id, s := range channels {
				s.Current = nil
				channels[id] = s
			}
		}
		for _, device := range sample.Devices {
			readings := device.PowerReadings
			if len(readings) == 0 && device.Power != nil {
				readings = []PowerReading{power("primary", "Primary power", "draw", device.Power)}
			}
			if len(readings) == 0 {
				continue
			}
			if out[device.ID] == nil {
				out[device.ID] = map[string]PowerStats{}
				values[device.ID] = map[string][]float64{}
			}
			for _, p := range readings {
				stat := out[device.ID][p.ID]
				stat.Name = p.Name
				stat.Mode = p.Mode
				stat.Current = watts(p.Watts)
				if stat.Current != nil {
					values[device.ID][p.ID] = append(values[device.ID][p.ID], *stat.Current)
				}
				out[device.ID][p.ID] = stat
			}
		}
	}
	for device, channels := range out {
		for id, stat := range channels {
			vals := values[device][id]
			stat.Samples = len(vals)
			stat.Mean = average(vals)
			for _, v := range vals {
				if stat.Min == nil || v < *stat.Min {
					stat.Min = Number(v)
				}
				if stat.Peak == nil || v > *stat.Peak {
					stat.Peak = Number(v)
				}
			}
			out[device][id] = stat
		}
	}
	return out
}
