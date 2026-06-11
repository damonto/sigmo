//go:build wifi_calling

package voicecodec

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	maxAMRStorageFrameBytes = 65
	amrNBEncodeMode         = 7
	amrWBEncodeMode         = 8
)

var (
	ErrAMRWASMUnavailable = errors.New("amr wasm codec is unavailable")
	ErrAMRCodecClosed     = errors.New("amr codec is closed")
)

type AMRCodecFactory struct {
	runtime wazero.Runtime
	module  wazero.CompiledModule
	close   sync.Once
}

func NewAMRCodecFactory(ctx context.Context, wasm []byte) (*AMRCodecFactory, error) {
	runtime := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("instantiate wasi: %w", err)
	}
	module, err := runtime.CompileModule(ctx, wasm)
	if err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("compile amr wasm: %w", err)
	}
	return &AMRCodecFactory{runtime: runtime, module: module}, nil
}

func (f *AMRCodecFactory) NewCodec(ctx context.Context, codec AMRCodec) (*WASMAMRCodec, error) {
	instance, err := f.runtime.InstantiateModule(ctx, f.module, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, fmt.Errorf("instantiate amr wasm module: %w", err)
	}
	c := &WASMAMRCodec{
		codec:  codec,
		module: instance,
		memory: instance.Memory(),
	}
	if err := c.bind(ctx); err != nil {
		if closeErr := c.Close(ctx); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close amr wasm codec: %w", closeErr))
		}
		return nil, err
	}
	return c, nil
}

func (f *AMRCodecFactory) Close(ctx context.Context) error {
	var err error
	f.close.Do(func() {
		err = f.runtime.Close(ctx)
	})
	return err
}

type WASMAMRCodec struct {
	codec  AMRCodec
	module api.Module
	memory api.Memory

	malloc api.Function
	free   api.Function

	decoderCreate  api.Function
	decoderDestroy api.Function
	decodeFunc     api.Function
	encoderCreate  api.Function
	encoderDestroy api.Function
	encodeFunc     api.Function

	decoderState uint32
	encoderState uint32
	framePtr     uint32
	pcmPtr       uint32
	outPtr       uint32
	closed       bool
}

func (c *WASMAMRCodec) Decode(ctx context.Context, frame AMRFrame) ([]int16, error) {
	if c.closed {
		return nil, ErrAMRCodecClosed
	}
	storage, err := (AMRStorageFrame{Codec: c.codec, Frame: frame}).MarshalBinary()
	if err != nil {
		return nil, err
	}
	if len(storage) > maxAMRStorageFrameBytes {
		return nil, ErrAMRFrameSizeMismatch
	}
	if !c.memory.Write(c.framePtr, storage) {
		return nil, ErrAMRWASMUnavailable
	}
	bfi := uint64(0)
	if !frame.Quality {
		bfi = 1
	}
	if _, err := c.decodeFunc.Call(ctx, uint64(c.decoderState), uint64(c.framePtr), uint64(c.pcmPtr), bfi); err != nil {
		return nil, fmt.Errorf("decode amr frame: %w", err)
	}
	samples := make([]int16, AMRSamplesPerFrame(c.codec))
	data, ok := c.memory.Read(c.pcmPtr, uint32(len(samples)*2))
	if !ok {
		return nil, ErrAMRWASMUnavailable
	}
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples, nil
}

func (c *WASMAMRCodec) Encode(ctx context.Context, samples []int16) ([]AMRFrame, error) {
	if c.closed {
		return nil, ErrAMRCodecClosed
	}
	frameSamples := AMRSamplesPerFrame(c.codec)
	if len(samples) == 0 || len(samples)%frameSamples != 0 {
		return nil, errors.New("amr encoder requires whole 20 ms PCM frames")
	}
	frames := []AMRFrame{}
	for offset := 0; offset < len(samples); offset += frameSamples {
		if !c.writePCM(samples[offset : offset+frameSamples]) {
			return nil, ErrAMRWASMUnavailable
		}
		var (
			result []uint64
			err    error
		)
		if c.codec == CodecAMR {
			result, err = c.encodeFunc.Call(ctx, uint64(c.encoderState), amrNBEncodeMode, uint64(c.pcmPtr), uint64(c.outPtr))
		} else {
			result, err = c.encodeFunc.Call(ctx, uint64(c.encoderState), amrWBEncodeMode, uint64(c.pcmPtr), uint64(c.outPtr), 0)
		}
		if err != nil {
			return nil, fmt.Errorf("encode amr frame: %w", err)
		}
		if len(result) == 0 || result[0] == 0 || result[0] > maxAMRStorageFrameBytes {
			return nil, errors.New("amr encoder returned an invalid frame")
		}
		data, ok := c.memory.Read(c.outPtr, uint32(result[0]))
		if !ok {
			return nil, ErrAMRWASMUnavailable
		}
		storage := AMRStorage{Codec: c.codec}
		if err := storage.UnmarshalBinary(data); err != nil {
			return nil, err
		}
		frames = append(frames, storage.Frames...)
	}
	return frames, nil
}

func (c *WASMAMRCodec) Close(ctx context.Context) error {
	if c.closed {
		return nil
	}
	c.closed = true
	var err error
	if c.decoderState != 0 {
		if _, destroyErr := c.decoderDestroy.Call(ctx, uint64(c.decoderState)); destroyErr != nil {
			err = errors.Join(err, fmt.Errorf("destroy amr decoder: %w", destroyErr))
		}
	}
	if c.encoderState != 0 {
		if _, destroyErr := c.encoderDestroy.Call(ctx, uint64(c.encoderState)); destroyErr != nil {
			err = errors.Join(err, fmt.Errorf("destroy amr encoder: %w", destroyErr))
		}
	}
	for _, ptr := range []uint32{c.framePtr, c.pcmPtr, c.outPtr} {
		if ptr != 0 {
			if _, freeErr := c.free.Call(ctx, uint64(ptr)); freeErr != nil {
				err = errors.Join(err, fmt.Errorf("free amr wasm memory: %w", freeErr))
			}
		}
	}
	if closeErr := c.module.Close(ctx); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close amr wasm module: %w", closeErr))
	}
	return err
}

func (c *WASMAMRCodec) bind(ctx context.Context) error {
	c.malloc = c.exportedFunc("malloc", "_malloc")
	c.free = c.exportedFunc("free", "_free")
	switch c.codec {
	case CodecAMR:
		c.decoderCreate = c.exportedFunc("sigmo_amrnb_decoder_create", "_sigmo_amrnb_decoder_create")
		c.decoderDestroy = c.exportedFunc("sigmo_amrnb_decoder_destroy", "_sigmo_amrnb_decoder_destroy")
		c.decodeFunc = c.exportedFunc("sigmo_amrnb_decode", "_sigmo_amrnb_decode")
		c.encoderCreate = c.exportedFunc("sigmo_amrnb_encoder_create", "_sigmo_amrnb_encoder_create")
		c.encoderDestroy = c.exportedFunc("sigmo_amrnb_encoder_destroy", "_sigmo_amrnb_encoder_destroy")
		c.encodeFunc = c.exportedFunc("sigmo_amrnb_encode", "_sigmo_amrnb_encode")
	case CodecAMRWB:
		c.decoderCreate = c.exportedFunc("sigmo_amrwb_decoder_create", "_sigmo_amrwb_decoder_create")
		c.decoderDestroy = c.exportedFunc("sigmo_amrwb_decoder_destroy", "_sigmo_amrwb_decoder_destroy")
		c.decodeFunc = c.exportedFunc("sigmo_amrwb_decode", "_sigmo_amrwb_decode")
		c.encoderCreate = c.exportedFunc("sigmo_amrwb_encoder_create", "_sigmo_amrwb_encoder_create")
		c.encoderDestroy = c.exportedFunc("sigmo_amrwb_encoder_destroy", "_sigmo_amrwb_encoder_destroy")
		c.encodeFunc = c.exportedFunc("sigmo_amrwb_encode", "_sigmo_amrwb_encode")
	default:
		return ErrAMRFrameTypeUnsupported
	}
	if c.malloc == nil || c.free == nil || c.decoderCreate == nil || c.decoderDestroy == nil || c.decodeFunc == nil || c.encoderCreate == nil || c.encoderDestroy == nil || c.encodeFunc == nil {
		return ErrAMRWASMUnavailable
	}

	decoder, err := c.decoderCreate.Call(ctx)
	if err != nil {
		return fmt.Errorf("create amr decoder: %w", err)
	}
	if len(decoder) == 0 || decoder[0] == 0 {
		return ErrAMRWASMUnavailable
	}
	c.decoderState = uint32(decoder[0])

	var encoder []uint64
	if c.codec == CodecAMR {
		encoder, err = c.encoderCreate.Call(ctx, 0)
	} else {
		encoder, err = c.encoderCreate.Call(ctx)
	}
	if err != nil {
		return fmt.Errorf("create amr encoder: %w", err)
	}
	if len(encoder) == 0 || encoder[0] == 0 {
		return ErrAMRWASMUnavailable
	}
	c.encoderState = uint32(encoder[0])
	framePtr, err := c.alloc(ctx, maxAMRStorageFrameBytes)
	if err != nil {
		return err
	}
	c.framePtr = framePtr
	pcmPtr, err := c.alloc(ctx, AMRSamplesPerFrame(c.codec)*2)
	if err != nil {
		return err
	}
	c.pcmPtr = pcmPtr
	outPtr, err := c.alloc(ctx, maxAMRStorageFrameBytes)
	if err != nil {
		return err
	}
	c.outPtr = outPtr
	return nil
}

func (c *WASMAMRCodec) alloc(ctx context.Context, size int) (uint32, error) {
	result, err := c.malloc.Call(ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("allocate amr wasm memory: %w", err)
	}
	if len(result) == 0 || result[0] == 0 {
		return 0, ErrAMRWASMUnavailable
	}
	return uint32(result[0]), nil
}

func (c *WASMAMRCodec) writePCM(samples []int16) bool {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	return c.memory.Write(c.pcmPtr, data)
}

func (c *WASMAMRCodec) exportedFunc(names ...string) api.Function {
	for _, name := range names {
		if fn := c.module.ExportedFunction(name); fn != nil {
			return fn
		}
	}
	return nil
}
