package wwan

import (
	"errors"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

// OpenMBIMSession opens a reusable MBIM session through an already-open modem.
// This preserves the modem generation's resolved direct or proxy access method.
func OpenMBIMSession(cfg Config, modem *wwanmodem.Modem) (*Session, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.PortType != PortTypeMBIM {
		return nil, ErrUnsupported
	}
	if modem == nil {
		return nil, errors.New("MBIM modem is required")
	}
	backend := newMBIMSessionWithMBIMOpener(cfg, modem.MBIMClient)
	return &Session{deviceOperations: deviceOperations{backend: backend}}, nil
}
