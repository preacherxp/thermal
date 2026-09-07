package thermal

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"
)

func TestSMCMonitoring(t *testing.T) {
	var data smcData
	if unsafe.Sizeof(data) != 80 || unsafe.Offsetof(data.Size) != 28 || unsafe.Offsetof(data.Result) != 40 || unsafe.Offsetof(data.Command) != 42 || unsafe.Offsetof(data.Bytes) != 48 {
		t.Fatal("SMC wire layout does not match the native ABI")
	}
	var raw [32]byte
	binary.BigEndian.PutUint16(raw[:2], 65*256+128)
	if got := smcTemperature(0x73703738, 2, raw); got == nil || *got != 65.5 {
		t.Fatalf("Intel temperature: %v", got)
	}
	binary.BigEndian.PutUint16(raw[:2], 0xff00)
	if smcTemperature(0x73703738, 2, raw) != nil {
		t.Fatal("inactive negative sensor accepted")
	}
	for _, v := range []float32{72.25, 0, -1, 151, float32(math.NaN()), float32(math.Inf(1))} {
		binary.LittleEndian.PutUint32(raw[:4], math.Float32bits(v))
		got := smcTemperature(0x666c7420, 4, raw)
		if v == 72.25 {
			if got == nil || *got != float64(v) {
				t.Fatalf("Apple Silicon temperature: %v", got)
			}
		} else if got != nil {
			t.Fatalf("invalid reading accepted: %v", v)
		}
	}
	if smcTemperature(0x666c7420, 2, raw) != nil || smcTemperature(0, 4, raw) != nil {
		t.Fatal("unknown SMC format accepted")
	}
	values := map[string]*float64{"Tg0a": Number(55), "Tg0b": Number(93)}
	read := func(key string) *float64 { return values[key] }
	keys := []string{"Tg0a", "Tg0b"}
	hottest := smcHottest(keys, read)
	s := Sample{Devices: []Device{{ID: "smc:gpu", Name: "Apple M4 Pro", Kind: "gpu", Temp: hottest}}}
	if hottest == nil || *hottest != 93 || gpuSensorID(s, "Apple M4 Pro") != "smc:gpu" || gpuGuard(s, "smc:gpu", 90, false) == "" {
		t.Fatal("hottest GPU sensor must trigger the stop limit")
	}
	delete(values, "Tg0b")
	s.Devices[0].Temp = smcHottest(keys, read)
	if s.Devices[0].Temp != nil || gpuGuard(s, "smc:gpu", 90, false) == "" {
		t.Fatal("sensor loss must not lower the reported maximum")
	}
	if gpuSensorID(s, "External GPU") != "" {
		t.Fatal("SMC reading matched to an unrelated GPU")
	}
}
