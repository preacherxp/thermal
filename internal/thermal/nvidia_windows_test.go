package thermal

import (
	"math"
	"testing"
	"unsafe"
)

func TestNVMLFieldABIAndUnits(t *testing.T) {
	if unsafe.Sizeof(nvField{}) != 40 || unsafe.Offsetof(nvField{}.Value) != 32 {
		t.Fatal("NVML field ABI mismatch")
	}
	for _, f := range []nvField{{Type: 0, Value: math.Float64bits(175000)}, {Type: 1, Value: 175000}, {Type: 2, Value: 175000}, {Type: 3, Value: 175000}} {
		if v := nvFieldWatts(f); v == nil || *v != 175 {
			t.Fatalf("mW conversion: %v", v)
		}
	}
	for _, f := range []nvField{{Status: 3, Value: 175000}, {Type: 0, Value: math.Float64bits(math.NaN())}, {Type: 4, Value: math.MaxUint64}, {Type: 99}} {
		if nvFieldWatts(f) != nil {
			t.Fatal("unsupported, negative, or invalid reading became watts")
		}
	}
}
