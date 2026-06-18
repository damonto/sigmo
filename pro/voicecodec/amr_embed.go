//go:build wifi_calling

package voicecodec

import (
	"context"
	_ "embed"
)

//go:embed assets/opencore-amr.wasm
var defaultAMRWASM []byte

func NewDefaultAMRCodecFactory(ctx context.Context) (*AMRCodecFactory, error) {
	return NewAMRCodecFactory(ctx, defaultAMRWASM)
}
