package thermal

import (
	"encoding/binary"
	"math"
)

type smcSampler struct {
	ready   bool
	cpuKey  string
	gpuKeys []string
}

// AppleSMC user-client ABI. Read operations only (commands 5, 8 and 9).
// CPU aggregates and dynamic GPU discovery: https://www.oshi.ooo/xref/oshi/util/platform/mac/SmcUtil.html.
// Explicit padding keeps the 80-byte wire layout identical on Intel and ARM.
type smcData struct {
	Key                     uint32
	Version                 [6]byte
	_                       [2]byte
	Limits                  [16]byte
	Size, Type              uint32
	Attributes              uint8
	_                       [3]byte
	Result, Status, Command uint8
	_                       byte
	Index                   uint32
	Bytes                   [32]byte
}

func smcTemperature(kind uint32, size uint32, data [32]byte) *float64 {
	var v float64
	switch {
	case kind == 0x73703738 && size == 2: // sp78, signed big-endian 8.8 fixed point
		v = float64(int16(binary.BigEndian.Uint16(data[:2]))) / 256
	case kind == 0x666c7420 && size == 4: // flt , little-endian IEEE754
		v = float64(math.Float32frombits(binary.LittleEndian.Uint32(data[:4])))
	default:
		return nil
	}
	// Zero is an inactive/unpopulated SMC sensor, not a usable die temperature.
	if v <= 0 {
		return nil
	}
	return temperature(Number(v))
}

// A lost sensor must stop monitoring, rather than silently lowering the maximum.
func smcHottest(keys []string, read func(string) *float64) *float64 {
	var hottest *float64
	for _, key := range keys {
		temp := read(key)
		if temp == nil {
			return nil
		}
		if hottest == nil || *temp > *hottest {
			hottest = temp
		}
	}
	return hottest
}
