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

// applyProfileDisplay reports whether branding was freshly read from the card.
// Cache hits are response-only: writing them back could undo an invalidation.
func (c *catalog) applyProfileDisplay(ctx context.Context, device *mmodem.Modem, snapshot mmodem.ModemSnapshot, sim *SlotResponse) bool {
	if c.profiles == nil || snapshot.Locked() || snapshot.SIMKind() != mmodem.SIMKindEUICC || snapshot.SIMIdentity.ICCID == "" {
		return false
	}
	if c.storage != nil {
		display, found, err := c.storage.ReadSIMDisplay(ctx, snapshot.SIMIdentity.ICCID)
		if err != nil {
			device.Logger().Warn("read cached SIM display", "error", err)
		}
		if found {
			sim.Label, sim.Icon = display.Label, display.Icon
			return false
		}
	}
	ctx, cancel := context.WithTimeout(ctx, profileIconTimeout)
	defer cancel()
	profile, err := c.profiles.ActiveProfile(ctx, device, snapshot.SIMIdentity)
	if err != nil {
		// Leaving the page cancels optional reads; it is not a device fault.
		if ctx.Err() == nil {
			device.Logger().Debug("read active SIM icon", "error", err)
		}
		return false
	}
	if profile == nil || device.Snapshot().SIMIdentity != snapshot.SIMIdentity {
		return false
	}
	if profile.ProfileNickname != "" {
		sim.Label = profile.ProfileNickname
	} else if profile.ProfileName != "" {
		sim.Label = profile.ProfileName
	}
	fileType := profile.Icon.FileType()
	if fileType == "" {
		return true
	}
	sim.Icon = fmt.Sprintf("data:%s;base64,%s", fileType, base64.StdEncoding.EncodeToString(profile.Icon))
	return true
}
