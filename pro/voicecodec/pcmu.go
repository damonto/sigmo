//go:build ims

package voicecodec

const pcmuBias = 0x84

// DecodePCMU converts G.711 u-law bytes to signed 16-bit mono PCM.
func DecodePCMU(payload []byte) []int16 {
	out := make([]int16, len(payload))
	for i, encoded := range payload {
		value := ^encoded
		sign := value & 0x80
		exponent := (value >> 4) & 0x07
		mantissa := value & 0x0f
		sample := int16((((int(mantissa) << 3) + pcmuBias) << exponent) - pcmuBias)
		if sign != 0 {
			sample = -sample
		}
		out[i] = sample
	}
	return out
}

// EncodePCMU converts signed 16-bit mono PCM to G.711 u-law bytes.
func EncodePCMU(samples []int16) []byte {
	out := make([]byte, len(samples))
	for i, sample := range samples {
		out[i] = encodePCMSample(sample)
	}
	return out
}

func encodePCMSample(sample int16) byte {
	value := int(sample)
	sign := 0
	if value < 0 {
		sign = 0x80
		value = -value
	}
	if value > 32635 {
		value = 32635
	}
	value += pcmuBias

	exponent := 7
	for mask := 0x4000; exponent > 0 && value&mask == 0; mask >>= 1 {
		exponent--
	}
	mantissa := (value >> (exponent + 3)) & 0x0f
	return byte(^(sign | (exponent << 4) | mantissa))
}
