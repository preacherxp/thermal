//go:build !darwin

package thermal

import (
	"context"
	"fmt"
)

func (*smcSampler) read(context.Context) ([]Device, error) {
	return nil, fmt.Errorf("AppleSMC requires macOS")
}
