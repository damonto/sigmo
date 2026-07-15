//go:build ims

package voicecodec

import (
	"context"
	"errors"
	"fmt"

	"github.com/damonto/ims-go/ims/voice/codec/amr/amrnb"
	"github.com/damonto/ims-go/ims/voice/codec/amr/amrwb"
)

var ErrAMRCodecUnsupported = errors.New("amr codec is not supported")

type AMRTranscoder struct {
	codec     AMRCodec
	nbEncoder *amrnb.Encoder
	nbDecoder *amrnb.Decoder
	wbEncoder *amrwb.Encoder
	wbDecoder *amrwb.Decoder
}

func NewAMRTranscoder(_ context.Context, codec AMRCodec, mode int) (*AMRTranscoder, error) {
	switch codec {
	case CodecAMR:
		encoder, err := amrnb.NewEncoder(amrnb.Config{Mode: amrnb.Mode(mode)})
		if err != nil {
			return nil, fmt.Errorf("create AMR encoder: %w", err)
		}
		return &AMRTranscoder{
			codec:     codec,
			nbEncoder: encoder,
			nbDecoder: amrnb.NewDecoder(),
		}, nil
	case CodecAMRWB:
		encoder, err := amrwb.NewEncoder(amrwb.Config{Mode: amrwb.Mode(mode)})
		if err != nil {
			return nil, fmt.Errorf("create AMR-WB encoder: %w", err)
		}
		return &AMRTranscoder{
			codec:     codec,
			wbEncoder: encoder,
			wbDecoder: amrwb.NewDecoder(),
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrAMRCodecUnsupported, codec)
	}
}

func (t *AMRTranscoder) Decode(_ context.Context, frame AMRFrame) ([]int16, error) {
	var (
		pcm []int16
		err error
	)
	switch t.codec {
	case CodecAMR:
		pcm, err = t.nbDecoder.Decode(nil, amrnb.Frame{
			Mode:    amrnb.Mode(frame.FrameType),
			Quality: frame.Quality,
			Data:    frame.Data,
		})
	case CodecAMRWB:
		pcm, err = t.wbDecoder.Decode(nil, amrwb.Frame{
			Mode:    amrwb.Mode(frame.FrameType),
			Quality: frame.Quality,
			Data:    frame.Data,
		})
	default:
		return nil, fmt.Errorf("%w: %q", ErrAMRCodecUnsupported, t.codec)
	}
	if err != nil {
		return nil, fmt.Errorf("decode AMR frame: %w", err)
	}
	return pcm, nil
}

func (t *AMRTranscoder) Encode(_ context.Context, samples []int16) ([]AMRFrame, error) {
	frameSamples := AMRSamplesPerFrame(t.codec)
	if len(samples) == 0 || len(samples)%frameSamples != 0 {
		return nil, errors.New("amr encoder requires whole 20 ms PCM frames")
	}
	frames := make([]AMRFrame, 0, len(samples)/frameSamples)
	for offset := 0; offset < len(samples); offset += frameSamples {
		frame, err := t.encodeFrame(samples[offset : offset+frameSamples])
		if err != nil {
			return nil, fmt.Errorf("encode AMR frame: %w", err)
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func (t *AMRTranscoder) encodeFrame(samples []int16) (AMRFrame, error) {
	switch t.codec {
	case CodecAMR:
		frame, err := t.nbEncoder.Encode(nil, samples)
		if err != nil {
			return AMRFrame{}, err
		}
		return AMRFrame{
			FrameType: int(frame.Mode),
			Quality:   frame.Quality,
			Data:      append([]byte(nil), frame.Data...),
		}, nil
	case CodecAMRWB:
		frame, err := t.wbEncoder.Encode(nil, samples)
		if err != nil {
			return AMRFrame{}, err
		}
		return AMRFrame{
			FrameType: int(frame.Mode),
			Quality:   frame.Quality,
			Data:      append([]byte(nil), frame.Data...),
		}, nil
	default:
		return AMRFrame{}, fmt.Errorf("%w: %q", ErrAMRCodecUnsupported, t.codec)
	}
}

func (*AMRTranscoder) Close(context.Context) error { return nil }
