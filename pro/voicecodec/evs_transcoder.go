//go:build ims

package voicecodec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/damonto/ims-go/ims/voice/codec/evs"
)

const (
	EVSSampleRate      = 16000
	EVSSamplesPerFrame = EVSSampleRate / 50
)

type Engine struct {
	engine *evs.Engine
}

func NewEngine(ctx context.Context) (*Engine, error) {
	engine, err := evs.NewEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("create speech codec engine: %w", err)
	}
	return &Engine{engine: engine}, nil
}

func (e *Engine) NewEVSTranscoder(ctx context.Context, cfg evs.EncoderConfig) (*EVSTranscoder, error) {
	if e == nil || e.engine == nil {
		return nil, errors.New("speech codec engine is unavailable")
	}
	codec, err := e.engine.NewCodec(ctx, evs.Config{
		SampleRate: EVSSampleRate,
		Encoder:    cfg,
	})
	if err != nil {
		return nil, fmt.Errorf("create EVS codec: %w", err)
	}
	return &EVSTranscoder{codec: codec}, nil
}

func (t *EVSTranscoder) Configure(ctx context.Context, cfg evs.EncoderConfig) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("EVS transcoder is closed")
	}
	if t.codec.EncoderConfig() == cfg {
		return nil
	}
	if err := t.codec.Configure(ctx, cfg); err != nil {
		return fmt.Errorf("configure EVS encoder: %w", err)
	}
	return nil
}

func (e *Engine) Close(ctx context.Context) error {
	if e == nil || e.engine == nil {
		return nil
	}
	return e.engine.Close(ctx)
}

type EVSFrame struct {
	FrameType int
	Quality   bool
	AMRWBIO   bool
	Data      []byte
}

type EVSTranscoder struct {
	mu     sync.Mutex
	codec  *evs.Codec
	closed bool
}

func (t *EVSTranscoder) Decode(ctx context.Context, frame EVSFrame) ([]int16, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errors.New("EVS transcoder is closed")
	}
	format := evs.Native
	if frame.AMRWBIO {
		format = evs.AMRWBIO
	}
	bits, err := evs.FrameBits(format, frame.FrameType)
	if err != nil {
		return nil, fmt.Errorf("read EVS frame size: %w", err)
	}
	pcm, err := t.codec.Decode(ctx, nil, evs.Frame{
		Format:    format,
		FrameType: frame.FrameType,
		Quality:   frame.Quality,
		Bits:      bits,
		Data:      frame.Data,
	})
	if err != nil {
		return nil, fmt.Errorf("decode EVS frame: %w", err)
	}
	return pcm, nil
}

func (t *EVSTranscoder) Encode(ctx context.Context, samples []int16) (EVSFrame, error) {
	if len(samples) != EVSSamplesPerFrame {
		return EVSFrame{}, errors.New("EVS encoder requires one whole 20 ms PCM frame")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return EVSFrame{}, errors.New("EVS transcoder is closed")
	}
	frame, err := t.codec.Encode(ctx, nil, samples)
	if err != nil {
		return EVSFrame{}, fmt.Errorf("encode EVS frame: %w", err)
	}
	return EVSFrame{
		FrameType: frame.FrameType,
		Quality:   frame.Quality,
		AMRWBIO:   frame.Format == evs.AMRWBIO,
		Data:      append([]byte(nil), frame.Data...),
	}, nil
}

func (t *EVSTranscoder) Close(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.codec.Close(ctx)
}
