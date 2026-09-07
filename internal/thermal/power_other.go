//go:build !darwin

package thermal

import (
	"context"
	"fmt"
)

func (*ioReportSampler) read(context.Context) (map[string][]PowerReading, error) {
	return nil, fmt.Errorf("IOReport requires macOS")
}
