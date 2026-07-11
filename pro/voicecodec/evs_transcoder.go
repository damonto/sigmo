//go:build ims

package voicecodec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	speechcodec "github.com/damonto/ims-go/ims/voice/codec"
)

const (
	EVSSampleRate      = 16000
	EVSSamplesPerFrame = EVSSampleRate / 50
)

type Engine struct {
	engine *speechcodec.Engine
}

func NewEngine(ctx context.Context) (*Engine, error) {
	engine, err := speechcodec.NewEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("create speech codec engine: %w", err)
	}
	return &Engine{engine: engine}, nil
}

func (e *Engine) NewEVSTranscoder(ctx context.Context) (*EVSTranscoder, error) {
	if e == nil || e.engine == nil {
		return nil, errors.New("speech codec engine is unavailable")
	}
	session, err := e.engine.NewSession(ctx, speechcodec.Config{
		Name: speechcodec.EVS,
		Mode: speechcodec.EVSMode13200,
		EVS: speechcodec.EVSConfig{
			SampleRate: EVSSampleRate,
			Bandwidth:  speechcodec.EVSBandwidthWB,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create EVS session: %w", err)
	}
	return &EVSTranscoder{session: session}, nil
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
	mu      sync.Mutex
	session speechcodec.Session
	closed  bool
}

func (t *EVSTranscoder) Decode(ctx context.Context, frame EVSFrame) ([]int16, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errors.New("EVS transcoder is closed")
	}
	format := speechcodec.EVSNative
	if frame.AMRWBIO {
		format = speechcodec.EVSAMRWBIO
	}
	bits, err := speechcodec.EVSFrameBits(format, speechcodec.Mode(frame.FrameType))
	if err != nil {
		return nil, fmt.Errorf("read EVS frame size: %w", err)
	}
	pcm, err := t.session.Decode(ctx, nil, speechcodec.Frame{
		Name:    speechcodec.EVS,
		Mode:    speechcodec.Mode(frame.FrameType),
		Quality: frame.Quality,
		Data:    frame.Data,
		EVS: speechcodec.EVSFrame{
			Format: format,
			Bits:   bits,
		},
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
	frame, err := t.session.Encode(ctx, nil, samples)
	if err != nil {
		return EVSFrame{}, fmt.Errorf("encode EVS frame: %w", err)
	}
	return EVSFrame{
		FrameType: int(frame.Mode),
		Quality:   frame.Quality,
		AMRWBIO:   frame.EVS.Format == speechcodec.EVSAMRWBIO,
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
	return t.session.Close(ctx)
}
