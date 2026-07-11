//go:build ims

package voicecodec

import (
	"context"
	"errors"
	"fmt"

	speechcodec "github.com/damonto/ims-go/ims/voice/codec"
)

var ErrAMRCodecUnsupported = errors.New("amr codec is not supported")

type AMRTranscoder struct {
	name    speechcodec.Name
	encoder speechcodec.Encoder
	decoder speechcodec.Decoder
}

func NewAMRTranscoder(ctx context.Context, codec AMRCodec) (*AMRTranscoder, error) {
	name, mode, err := amrCodecConfig(codec)
	if err != nil {
		return nil, err
	}
	encoder, err := speechcodec.NewEncoder(ctx, speechcodec.Config{Name: name, Mode: mode})
	if err != nil {
		return nil, fmt.Errorf("create AMR encoder: %w", err)
	}
	decoder, err := speechcodec.NewDecoder(ctx, name)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create AMR decoder: %w", err), encoder.Close(ctx))
	}
	return &AMRTranscoder{
		name:    name,
		encoder: encoder,
		decoder: decoder,
	}, nil
}

func (t *AMRTranscoder) Decode(ctx context.Context, frame AMRFrame) ([]int16, error) {
	pcm, err := t.decoder.Decode(ctx, nil, speechcodec.Frame{
		Name:    t.name,
		Mode:    speechcodec.Mode(frame.FrameType),
		Quality: frame.Quality,
		Data:    frame.Data,
	})
	if err != nil {
		return nil, fmt.Errorf("decode AMR frame: %w", err)
	}
	return pcm, nil
}

func (t *AMRTranscoder) Encode(ctx context.Context, samples []int16) ([]AMRFrame, error) {
	frameSamples := t.encoder.FrameSamples()
	if len(samples) == 0 || len(samples)%frameSamples != 0 {
		return nil, errors.New("amr encoder requires whole 20 ms PCM frames")
	}
	frames := make([]AMRFrame, 0, len(samples)/frameSamples)
	for offset := 0; offset < len(samples); offset += frameSamples {
		frame, err := t.encoder.Encode(ctx, nil, samples[offset:offset+frameSamples])
		if err != nil {
			return nil, fmt.Errorf("encode AMR frame: %w", err)
		}
		frames = append(frames, AMRFrame{
			FrameType: int(frame.Mode),
			Quality:   frame.Quality,
			Data:      append([]byte(nil), frame.Data...),
		})
	}
	return frames, nil
}

func (t *AMRTranscoder) Close(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return errors.Join(t.encoder.Close(ctx), t.decoder.Close(ctx))
}

func amrCodecConfig(codec AMRCodec) (speechcodec.Name, speechcodec.Mode, error) {
	switch codec {
	case CodecAMR:
		return speechcodec.AMR, speechcodec.AMRMode1220, nil
	case CodecAMRWB:
		return speechcodec.AMRWB, speechcodec.AMRWBMode2385, nil
	default:
		return "", 0, fmt.Errorf("%w: %q", ErrAMRCodecUnsupported, codec)
	}
}
