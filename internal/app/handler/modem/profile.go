package modem

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	sgp22 "github.com/damonto/euicc-go/v2"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type activeProfileReader interface {
	ActiveProfile(context.Context, *mmodem.Modem, mmodem.SIMIdentity) (*sgp22.ProfileInfo, error)
}

// Optional branding must not make a busy or unavailable eUICC fail the overview.
const profileIconTimeout = 5 * time.Second

func (c *catalog) profileIcon(ctx context.Context, device *mmodem.Modem, snapshot mmodem.ModemSnapshot) string {
	if c.profiles == nil || snapshot.Locked() || snapshot.SIMKind() != mmodem.SIMKindEUICC || snapshot.SIMIdentity.ICCID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, profileIconTimeout)
	defer cancel()
	profile, err := c.profiles.ActiveProfile(ctx, device, snapshot.SIMIdentity)
	if err != nil {
		// Leaving the page cancels optional reads; it is not a device fault.
		if ctx.Err() == nil {
			device.Logger().Debug("read active SIM icon", "error", err)
		}
		return ""
	}
	if profile == nil || device.Snapshot().SIMIdentity != snapshot.SIMIdentity {
		return ""
	}
	fileType := profile.Icon.FileType()
	if fileType == "" {
		return ""
	}
	return fmt.Sprintf("data:%s;base64,%s", fileType, base64.StdEncoding.EncodeToString(profile.Icon))
}
