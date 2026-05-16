package msisdn

import "github.com/damonto/sigmo/internal/pkg/modem/at"

type CRSM struct {
	commander commandRunner
}

func NewCRSM(conn *at.AT) Runner {
	return &CRSM{commander: at.NewCRSM(conn)}
}

func (r *CRSM) Select() ([]byte, error) {
	command := at.CRSMCommand{Instruction: at.CRSMGetResponse, FileID: 0x6F40}
	return r.commander.Run(command.Bytes())
}

func (r *CRSM) Run(data []byte) error {
	command := at.CRSMCommand{
		Instruction: at.CRSMUpdateRecord,
		FileID:      0x6F40,
		P1:          1,
		P2:          4,
		Data:        data,
	}
	_, err := r.commander.Run(command.Bytes())
	return err
}
