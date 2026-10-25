package voicecodec

import (
	"bytes"
	"errors"
	"testing"
)

func TestAMROctetAlignedPayload(t *testing.T) {
	tests := []struct {
		name   string
		codec  AMRCodec
		frames []AMRFrame
	}{
		{
			name:  "amr sid",
			codec: CodecAMR,
			frames: []AMRFrame{
				{FrameType: 8, Quality: true, Data: []byte{1, 2, 3, 4, 5}},
			},
		},
		{
			name:  "amr wb sid",
			codec: CodecAMRWB,
			frames: []AMRFrame{
				{FrameType: 9, Quality: true, Data: []byte{1, 2, 3, 4, 5}},
			},
		},
		{
			name:  "multiple frames",
			codec: CodecAMR,
			frames: []AMRFrame{
				{FrameType: 8, Quality: true, Data: []byte{1, 2, 3, 4, 5}},
				{FrameType: 15, Quality: false, Data: []byte{}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := (AMROctetAlignedPayload{Codec: tt.codec, Frames: tt.frames}).MarshalBinary()
			if err != nil {
				t.Fatalf("AMROctetAlignedPayload.MarshalBinary() error = %v", err)
			}
			got := AMROctetAlignedPayload{Codec: tt.codec}
			if err := got.UnmarshalBinary(payload); err != nil {
				t.Fatalf("AMROctetAlignedPayload.UnmarshalBinary() error = %v", err)
			}
			assertAMRFrames(t, got.Frames, tt.frames)
		})
	}
}

func TestAMRBandwidthEfficientPayload(t *testing.T) {
	tests := []struct {
		name   string
		codec  AMRCodec
		frames []AMRFrame
	}{
		{
			name:  "amr sid clears unused padding bit",
			codec: CodecAMR,
			frames: []AMRFrame{
				{FrameType: 8, Quality: true, Data: []byte{1, 2, 3, 4, 4}},
			},
		},
		{
			name:  "amr wb sid",
			codec: CodecAMRWB,
			frames: []AMRFrame{
				{FrameType: 9, Quality: true, Data: []byte{1, 2, 3, 4, 5}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := (AMRBandwidthEfficientPayload{Codec: tt.codec, Frames: tt.frames}).MarshalBinary()
			if err != nil {
				t.Fatalf("AMRBandwidthEfficientPayload.MarshalBinary() error = %v", err)
			}
			got := AMRBandwidthEfficientPayload{Codec: tt.codec}
			if err := got.UnmarshalBinary(payload); err != nil {
				t.Fatalf("AMRBandwidthEfficientPayload.UnmarshalBinary() error = %v", err)
			}
			assertAMRFrames(t, got.Frames, tt.frames)
		})
	}
}

func TestAMRPayloadErrors(t *testing.T) {
	tests := []struct {
		name    string
		run     func() error
		wantErr error
	}{
		{
			name: "octet aligned requires frames",
			run: func() error {
				_, err := (AMROctetAlignedPayload{Codec: CodecAMR}).MarshalBinary()
				return err
			},
			wantErr: ErrAMRFramesRequired,
		},
		{
			name: "octet aligned rejects wrong size",
			run: func() error {
				_, err := (AMROctetAlignedPayload{
					Codec:  CodecAMR,
					Frames: []AMRFrame{{FrameType: 8, Quality: true, Data: []byte{1}}},
				}).MarshalBinary()
				return err
			},
			wantErr: ErrAMRFrameSizeMismatch,
		},
		{
			name: "bandwidth efficient rejects truncated payload",
			run: func() error {
				payload := AMRBandwidthEfficientPayload{Codec: CodecAMR}
				return payload.UnmarshalBinary(nil)
			},
			wantErr: ErrAMRPayloadTruncated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAMRStorage(t *testing.T) {
	tests := []struct {
		name   string
		codec  AMRCodec
		frames []AMRFrame
	}{
		{
			name:  "amr",
			codec: CodecAMR,
			frames: []AMRFrame{
				{FrameType: 8, Quality: true, Data: []byte{1, 2, 3, 4, 5}},
			},
		},
		{
			name:  "amr wb",
			codec: CodecAMRWB,
			frames: []AMRFrame{
				{FrameType: 9, Quality: true, Data: []byte{1, 2, 3, 4, 5}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := AMRStorage{Codec: tt.codec, Frames: tt.frames, IncludeHeader: true}
			data, err := storage.MarshalBinary()
			if err != nil {
				t.Fatalf("AMRStorage.MarshalBinary() error = %v", err)
			}

			var got AMRStorage
			if _, err := got.ReadFrom(bytes.NewReader(data)); err != nil {
				t.Fatalf("AMRStorage.ReadFrom() error = %v", err)
			}
			if got.Codec != tt.codec {
				t.Fatalf("AMRStorage.Codec = %q, want %q", got.Codec, tt.codec)
			}
			assertAMRFrames(t, got.Frames, tt.frames)

			var out bytes.Buffer
			if _, err := got.WriteTo(&out); err != nil {
				t.Fatalf("AMRStorage.WriteTo() error = %v", err)
			}
			if !bytes.Equal(out.Bytes(), data) {
				t.Fatalf("AMRStorage.WriteTo() = %v, want %v", out.Bytes(), data)
			}
		})
	}
}

func TestAMRStorageMarshalRequiresCodec(t *testing.T) {
	tests := []struct {
		name    string
		storage AMRStorage
		wantErr error
	}{
		{
			name:    "headerless frames require codec",
			storage: AMRStorage{Frames: []AMRFrame{{FrameType: 8, Quality: true, Data: []byte{1, 2, 3, 4, 5}}}},
			wantErr: ErrAMRStorageHeaderMissing,
		},
		{
			name:    "header requires codec",
			storage: AMRStorage{IncludeHeader: true},
			wantErr: ErrAMRStorageHeaderMissing,
		},
		{
			name: "invalid codec",
			storage: AMRStorage{
				Codec:  "EVS",
				Frames: []AMRFrame{{FrameType: 8, Quality: true, Data: []byte{1, 2, 3, 4, 5}}},
			},
			wantErr: ErrAMRStorageHeaderMissing,
		},
		{
			name: "empty storage can omit codec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.storage.MarshalBinary()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AMRStorage.MarshalBinary() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func assertAMRFrames(t *testing.T, got []AMRFrame, want []AMRFrame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("frames length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].FrameType != want[i].FrameType || got[i].Quality != want[i].Quality {
			t.Fatalf("frame[%d] header = %+v, want %+v", i, got[i], want[i])
		}
		if string(got[i].Data) != string(want[i].Data) {
			t.Fatalf("frame[%d] data = %v, want %v", i, got[i].Data, want[i].Data)
		}
	}
}
