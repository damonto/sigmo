//go:build ims

package voicecodec

import "errors"

var ErrInvalidSampleRate = errors.New("sample rates must be positive")

// ResampleLinear converts mono PCM between sample rates using linear interpolation.
func ResampleLinear(input []int16, fromRate int, toRate int) ([]int16, error) {
	if fromRate <= 0 || toRate <= 0 {
		return nil, ErrInvalidSampleRate
	}
	if len(input) == 0 {
		return []int16{}, nil
	}
	if fromRate == toRate {
		out := make([]int16, len(input))
		copy(out, input)
		return out, nil
	}

	outputLen := max(1, (len(input)*toRate+fromRate/2)/fromRate)
	out := make([]int16, outputLen)
	for i := range outputLen {
		sourceNumerator := i * fromRate
		left := sourceNumerator / toRate
		if left >= len(input) {
			left = len(input) - 1
		}
		right := min(left+1, len(input)-1)
		remainder := sourceNumerator % toRate
		leftWeight := toRate - remainder
		rightWeight := remainder
		out[i] = int16((int(input[left])*leftWeight + int(input[right])*rightWeight) / toRate)
	}
	return out, nil
}
