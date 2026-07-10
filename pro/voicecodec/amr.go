//go:build ims

package voicecodec

import (
	"bufio"
	"errors"
	"io"
)

type AMRCodec string

const (
	CodecAMR   AMRCodec = "AMR"
	CodecAMRWB AMRCodec = "AMR-WB"
)

type AMRFrame struct {
	FrameType int
	Quality   bool
	Data      []byte
}

type AMRPayload struct {
	Codec        AMRCodec
	OctetAligned bool
	Frames       []AMRFrame
}

func (p AMRPayload) MarshalBinary() ([]byte, error) {
	if p.OctetAligned {
		return AMROctetAlignedPayload{Codec: p.Codec, Frames: p.Frames}.MarshalBinary()
	}
	return AMRBandwidthEfficientPayload{Codec: p.Codec, Frames: p.Frames}.MarshalBinary()
}

func (p *AMRPayload) UnmarshalBinary(data []byte) error {
	if p.OctetAligned {
		payload := AMROctetAlignedPayload{Codec: p.Codec}
		if err := payload.UnmarshalBinary(data); err != nil {
			return err
		}
		p.Frames = payload.Frames
		return nil
	}

	payload := AMRBandwidthEfficientPayload{Codec: p.Codec}
	if err := payload.UnmarshalBinary(data); err != nil {
		return err
	}
	p.Frames = payload.Frames
	return nil
}

type AMROctetAlignedPayload struct {
	Codec  AMRCodec
	Frames []AMRFrame
}

type AMRBandwidthEfficientPayload struct {
	Codec  AMRCodec
	Frames []AMRFrame
}

type AMRStorageFrame struct {
	Codec AMRCodec
	Frame AMRFrame
}

type AMRStorage struct {
	Codec         AMRCodec
	Frames        []AMRFrame
	IncludeHeader bool
}

var (
	amrStorageHeader   = []byte("#!AMR\n")
	amrWBStorageHeader = []byte("#!AMR-WB\n")

	ErrAMRPayloadShort         = errors.New("amr payload is too short")
	ErrAMRPayloadTruncated     = errors.New("amr payload is truncated")
	ErrAMRTOCTruncated         = errors.New("amr table of contents is truncated")
	ErrAMRSpeechDataTruncated  = errors.New("amr speech data is truncated")
	ErrAMRFrameTypeUnsupported = errors.New("amr frame type is not supported")
	ErrAMRFrameSizeMismatch    = errors.New("amr frame size does not match frame type")
	ErrAMRFramesRequired       = errors.New("amr payload requires at least one frame")
	ErrAMRStorageHeaderMissing = errors.New("amr data is missing a file header")
)

var amrFrameBits = []int{95, 103, 118, 134, 148, 159, 204, 244, 39}
var amrWBFrameBits = []int{132, 177, 253, 285, 317, 365, 397, 461, 477, 40}
var amrFrameBytes = []int{12, 13, 15, 17, 19, 20, 26, 31, 5}
var amrWBFrameBytes = []int{17, 23, 32, 36, 40, 46, 50, 58, 60, 5}

const noModeRequest = 15

func (p AMROctetAlignedPayload) MarshalBinary() ([]byte, error) {
	if len(p.Frames) == 0 {
		return nil, ErrAMRFramesRequired
	}
	speechBytes := 0
	for _, frame := range p.Frames {
		want := AMRFrameBytes(p.Codec, frame.FrameType)
		if want < 0 {
			return nil, ErrAMRFrameTypeUnsupported
		}
		if len(frame.Data) != want {
			return nil, ErrAMRFrameSizeMismatch
		}
		speechBytes += len(frame.Data)
	}
	out := make([]byte, 1+len(p.Frames)+speechBytes)
	out[0] = noModeRequest << 4
	offset := 1
	for i, frame := range p.Frames {
		toc := byte((frame.FrameType & 0x0f) << 3)
		if i < len(p.Frames)-1 {
			toc |= 0x80
		}
		if frame.Quality {
			toc |= 0x04
		}
		out[offset] = toc
		offset++
	}
	for _, frame := range p.Frames {
		copy(out[offset:], frame.Data)
		offset += len(frame.Data)
	}
	return out, nil
}

func (p *AMROctetAlignedPayload) UnmarshalBinary(payload []byte) error {
	if len(payload) < 2 {
		return ErrAMRPayloadShort
	}
	frames := []AMRFrame{}
	offset := 1
	hasMore := true
	for hasMore {
		if offset >= len(payload) {
			return ErrAMRTOCTruncated
		}
		toc := payload[offset]
		offset++
		hasMore = toc&0x80 != 0
		frameType := int((toc >> 3) & 0x0f)
		quality := toc&0x04 != 0
		size := AMRFrameBytes(p.Codec, frameType)
		if size < 0 {
			return ErrAMRFrameTypeUnsupported
		}
		frames = append(frames, AMRFrame{
			FrameType: frameType,
			Quality:   quality,
			Data:      make([]byte, size),
		})
	}
	for i := range frames {
		end := offset + len(frames[i].Data)
		if end > len(payload) {
			return ErrAMRSpeechDataTruncated
		}
		copy(frames[i].Data, payload[offset:end])
		offset = end
	}
	p.Frames = frames
	return nil
}

func (p AMRBandwidthEfficientPayload) MarshalBinary() ([]byte, error) {
	if len(p.Frames) == 0 {
		return nil, ErrAMRFramesRequired
	}
	writer := bitWriter{}
	writer.writeBits(noModeRequest, 4)
	for i, frame := range p.Frames {
		bits := amrFrameBitCount(p.Codec, frame.FrameType)
		if bits < 0 {
			return nil, ErrAMRFrameTypeUnsupported
		}
		if len(frame.Data) != bytesForBits(bits) {
			return nil, ErrAMRFrameSizeMismatch
		}
		if i < len(p.Frames)-1 {
			writer.writeBits(1, 1)
		} else {
			writer.writeBits(0, 1)
		}
		writer.writeBits(frame.FrameType&0x0f, 4)
		if frame.Quality {
			writer.writeBits(1, 1)
		} else {
			writer.writeBits(0, 1)
		}
	}
	for _, frame := range p.Frames {
		writer.writeBytes(frame.Data, amrFrameBitCount(p.Codec, frame.FrameType))
	}
	return writer.bytes(), nil
}

func (p *AMRBandwidthEfficientPayload) UnmarshalBinary(payload []byte) error {
	reader := bitReader{data: payload}
	if _, err := reader.readBits(4); err != nil {
		return err
	}
	frames := []AMRFrame{}
	hasMore := true
	for hasMore {
		f, err := reader.readBits(1)
		if err != nil {
			return err
		}
		frameType, err := reader.readBits(4)
		if err != nil {
			return err
		}
		quality, err := reader.readBits(1)
		if err != nil {
			return err
		}
		bits := amrFrameBitCount(p.Codec, int(frameType))
		if bits < 0 {
			return ErrAMRFrameTypeUnsupported
		}
		frames = append(frames, AMRFrame{
			FrameType: int(frameType),
			Quality:   quality != 0,
			Data:      make([]byte, bytesForBits(bits)),
		})
		hasMore = f != 0
	}
	for i := range frames {
		bits := amrFrameBitCount(p.Codec, frames[i].FrameType)
		data, err := reader.readBytes(bits)
		if err != nil {
			return err
		}
		copy(frames[i].Data, data)
	}
	p.Frames = frames
	return nil
}

func AMRFrameBytes(codec AMRCodec, frameType int) int {
	if frameType == 15 || codec == CodecAMRWB && frameType == 14 {
		return 0
	}
	switch codec {
	case CodecAMR:
		if frameType >= 0 && frameType < len(amrFrameBytes) {
			return amrFrameBytes[frameType]
		}
	case CodecAMRWB:
		if frameType >= 0 && frameType < len(amrWBFrameBytes) {
			return amrWBFrameBytes[frameType]
		}
	}
	return -1
}

func AMRSampleRate(codec AMRCodec) int {
	if codec == CodecAMRWB {
		return 16000
	}
	return 8000
}

func AMRSamplesPerFrame(codec AMRCodec) int {
	if codec == CodecAMRWB {
		return 320
	}
	return 160
}

func (f AMRStorageFrame) MarshalBinary() ([]byte, error) {
	if f.Codec != "" {
		want := AMRFrameBytes(f.Codec, f.Frame.FrameType)
		if want < 0 {
			return nil, ErrAMRFrameTypeUnsupported
		}
		if len(f.Frame.Data) != want {
			return nil, ErrAMRFrameSizeMismatch
		}
	}
	out := make([]byte, 1+len(f.Frame.Data))
	out[0] = byte((f.Frame.FrameType << 3) & 0x78)
	if f.Frame.Quality {
		out[0] |= 0x04
	}
	copy(out[1:], f.Frame.Data)
	return out, nil
}

func (f *AMRStorageFrame) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		return ErrAMRSpeechDataTruncated
	}
	header := data[0]
	frameType := int((header >> 3) & 0x0f)
	quality := header&0x04 != 0
	size := AMRFrameBytes(f.Codec, frameType)
	if size < 0 {
		return ErrAMRFrameTypeUnsupported
	}
	if len(data) < 1+size {
		return ErrAMRSpeechDataTruncated
	}
	if len(data) != 1+size {
		return ErrAMRFrameSizeMismatch
	}
	frameData := make([]byte, size)
	copy(frameData, data[1:])
	f.Frame = AMRFrame{FrameType: frameType, Quality: quality, Data: frameData}
	return nil
}

func (s AMRStorage) MarshalBinary() ([]byte, error) {
	switch s.Codec {
	case CodecAMR, CodecAMRWB:
	default:
		if len(s.Frames) > 0 || s.IncludeHeader {
			return nil, ErrAMRStorageHeaderMissing
		}
	}

	size := 0
	if s.IncludeHeader {
		switch s.Codec {
		case CodecAMR:
			size += len(amrStorageHeader)
		case CodecAMRWB:
			size += len(amrWBStorageHeader)
		}
	}
	for _, frame := range s.Frames {
		size += 1 + len(frame.Data)
	}
	out := make([]byte, 0, size)
	if s.IncludeHeader {
		if s.Codec == CodecAMRWB {
			out = append(out, amrWBStorageHeader...)
		} else {
			out = append(out, amrStorageHeader...)
		}
	}
	for _, frame := range s.Frames {
		data, err := (AMRStorageFrame{Codec: s.Codec, Frame: frame}).MarshalBinary()
		if err != nil {
			return nil, err
		}
		out = append(out, data...)
	}
	return out, nil
}

func (s *AMRStorage) UnmarshalBinary(data []byte) error {
	codec, offset, err := storageCodec(s.Codec, data)
	if err != nil {
		return err
	}
	includeHeader := offset > 0
	frames := []AMRFrame{}
	for offset < len(data) {
		header := data[offset]
		offset++
		frameType := int((header >> 3) & 0x0f)
		quality := header&0x04 != 0
		size := AMRFrameBytes(codec, frameType)
		if size < 0 {
			return ErrAMRFrameTypeUnsupported
		}
		end := offset + size
		if end > len(data) {
			return ErrAMRSpeechDataTruncated
		}
		frameData := make([]byte, size)
		copy(frameData, data[offset:end])
		frames = append(frames, AMRFrame{FrameType: frameType, Quality: quality, Data: frameData})
		offset = end
	}
	s.Codec = codec
	s.Frames = frames
	s.IncludeHeader = includeHeader
	return nil
}

func (s *AMRStorage) ReadFrom(r io.Reader) (int64, error) {
	counter := &countingReader{r: r}
	reader := bufio.NewReader(counter)
	codec, includeHeader, err := readAMRStorageHeader(reader, s.Codec)
	if err != nil {
		return counter.n, err
	}

	frames := []AMRFrame{}
	for {
		header, err := reader.ReadByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return counter.n, err
		}
		frameType := int((header >> 3) & 0x0f)
		quality := header&0x04 != 0
		size := AMRFrameBytes(codec, frameType)
		if size < 0 {
			return counter.n, ErrAMRFrameTypeUnsupported
		}
		frameData := make([]byte, size)
		if _, err := io.ReadFull(reader, frameData); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return counter.n, ErrAMRSpeechDataTruncated
			}
			return counter.n, err
		}
		frames = append(frames, AMRFrame{FrameType: frameType, Quality: quality, Data: frameData})
	}
	s.Codec = codec
	s.Frames = frames
	s.IncludeHeader = includeHeader
	return counter.n, nil
}

func (s AMRStorage) WriteTo(w io.Writer) (int64, error) {
	var written int64
	switch s.Codec {
	case CodecAMR, CodecAMRWB:
	default:
		if len(s.Frames) > 0 || s.IncludeHeader {
			return 0, ErrAMRStorageHeaderMissing
		}
	}
	if s.IncludeHeader {
		header := amrStorageHeader
		if s.Codec == CodecAMRWB {
			header = amrWBStorageHeader
		}
		n, err := writeFull(w, header)
		written += n
		if err != nil {
			return written, err
		}
	}
	for _, frame := range s.Frames {
		data, err := (AMRStorageFrame{Codec: s.Codec, Frame: frame}).MarshalBinary()
		if err != nil {
			return written, err
		}
		n, err := writeFull(w, data)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func readAMRStorageHeader(reader *bufio.Reader, fallback AMRCodec) (AMRCodec, bool, error) {
	if hasBufferedPrefix(reader, amrWBStorageHeader) {
		_, _ = reader.Discard(len(amrWBStorageHeader))
		return CodecAMRWB, true, nil
	}
	if hasBufferedPrefix(reader, amrStorageHeader) {
		_, _ = reader.Discard(len(amrStorageHeader))
		return CodecAMR, true, nil
	}
	if fallback == CodecAMR || fallback == CodecAMRWB {
		return fallback, false, nil
	}
	return "", false, ErrAMRStorageHeaderMissing
}

func hasBufferedPrefix(reader *bufio.Reader, prefix []byte) bool {
	data, err := reader.Peek(len(prefix))
	return (err == nil || errors.Is(err, io.EOF)) && hasPrefix(data, prefix)
}

func writeFull(w io.Writer, data []byte) (int64, error) {
	var written int64
	for len(data) > 0 {
		n, err := w.Write(data)
		written += int64(n)
		data = data[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func storageCodec(codec AMRCodec, data []byte) (AMRCodec, int, error) {
	if hasPrefix(data, amrStorageHeader) {
		return CodecAMR, len(amrStorageHeader), nil
	}
	if hasPrefix(data, amrWBStorageHeader) {
		return CodecAMRWB, len(amrWBStorageHeader), nil
	}
	if codec == CodecAMR || codec == CodecAMRWB {
		return codec, 0, nil
	}
	return "", 0, ErrAMRStorageHeaderMissing
}

func hasPrefix(data []byte, prefix []byte) bool {
	if len(data) < len(prefix) {
		return false
	}
	for i, value := range prefix {
		if data[i] != value {
			return false
		}
	}
	return true
}

func amrFrameBitCount(codec AMRCodec, frameType int) int {
	if frameType == 15 || codec == CodecAMRWB && frameType == 14 {
		return 0
	}
	switch codec {
	case CodecAMR:
		if frameType >= 0 && frameType < len(amrFrameBits) {
			return amrFrameBits[frameType]
		}
	case CodecAMRWB:
		if frameType >= 0 && frameType < len(amrWBFrameBits) {
			return amrWBFrameBits[frameType]
		}
	}
	return -1
}

func bytesForBits(bits int) int {
	return (bits + 7) / 8
}

type bitReader struct {
	data     []byte
	position int
}

func (r *bitReader) readBits(count int) (int, error) {
	if count < 0 || r.position+count > len(r.data)*8 {
		return 0, ErrAMRPayloadTruncated
	}
	value := 0
	for range count {
		byteIndex := r.position / 8
		bitIndex := 7 - (r.position % 8)
		value = value<<1 | int((r.data[byteIndex]>>bitIndex)&1)
		r.position++
	}
	return value, nil
}

func (r *bitReader) readBytes(bits int) ([]byte, error) {
	out := make([]byte, bytesForBits(bits))
	for i := range bits {
		bit, err := r.readBits(1)
		if err != nil {
			return nil, err
		}
		if bit != 0 {
			out[i/8] |= 1 << (7 - (i % 8))
		}
	}
	return out, nil
}

type bitWriter struct {
	data     []byte
	position int
}

func (w *bitWriter) writeBits(value int, count int) {
	for i := count - 1; i >= 0; i-- {
		w.writeBit(value&(1<<i) != 0)
	}
}

func (w *bitWriter) writeBytes(data []byte, bits int) {
	for i := range bits {
		w.writeBit(data[i/8]&(1<<(7-(i%8))) != 0)
	}
}

func (w *bitWriter) bytes() []byte {
	out := make([]byte, len(w.data))
	copy(out, w.data)
	return out
}

func (w *bitWriter) writeBit(value bool) {
	byteIndex := w.position / 8
	if byteIndex == len(w.data) {
		w.data = append(w.data, 0)
	}
	if value {
		w.data[byteIndex] |= 1 << (7 - (w.position % 8))
	}
	w.position++
}
