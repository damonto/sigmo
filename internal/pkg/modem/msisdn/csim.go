package msisdn

import "github.com/damonto/sigmo/internal/pkg/modem/at"

type CSIM struct {
	commander commandRunner
}

func NewCSIM(conn *at.AT) Runner {
	return &CSIM{commander: at.NewCSIM(conn)}
}

func (r *CSIM) Run(data []byte) error {
	_, err := r.commander.Run(append([]byte{0x00, 0xDC, 0x01, 0x04, byte(len(data))}, data...))
	return err
}

func (r *CSIM) Select() ([]byte, error) {
	return r.commander.Run([]byte{0x00, 0xA4, 0x08, 0x04, 0x04, 0x7F, 0xFF, 0x6F, 0x40})
}
