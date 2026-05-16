package network

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var errOperatorCodeRequired = errors.New("operator code is required")

func (n *network) Register(ctx context.Context, modem *mmodem.Modem, operatorCode string) error {
	operatorCode = strings.TrimSpace(operatorCode)
	if operatorCode == "" {
		return errOperatorCodeRequired
	}
	if err := modem.ThreeGPP().RegisterNetwork(ctx, operatorCode); err != nil {
		return fmt.Errorf("register network %s: %w", operatorCode, err)
	}
	return nil
}
