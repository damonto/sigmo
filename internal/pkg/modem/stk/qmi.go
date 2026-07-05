package stk

import (
	"context"
	"fmt"
	"slices"

	"github.com/damonto/uicc-go/qcom/uim"
	stkpkg "github.com/damonto/uicc-go/usim/stk"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type qmiCATSession interface {
	Configuration(context.Context) (uim.CATConfiguration, error)
	SetConfiguration(context.Context, uim.CATConfiguration) error
	ForceClaimEvents(context.Context, uim.CATEventClaimConfig) (uim.CATEventClaim, error)
}

func ensureCATReady(ctx context.Context, modem *mmodem.Modem, cat qmiCATSession) error {
	profile := terminalProfile()
	profileData := profile.Bytes()

	config, err := cat.Configuration(ctx)
	if err != nil {
		return fmt.Errorf("read QMI CAT configuration: %w", err)
	}
	profileChanged := !slices.Equal(config.CustomProfile, profileData)
	if config.Mode != uim.CATConfigCustomRaw || profileChanged {
		modem.Logger().Info(
			"set QMI CAT configuration",
			"from", config.Mode,
			"to", uim.CATConfigCustomRaw,
			"profileChanged", profileChanged,
		)
		if err := cat.SetConfiguration(ctx, uim.CATConfiguration{
			Mode:          uim.CATConfigCustomRaw,
			CustomProfile: profileData,
		}); err != nil {
			return fmt.Errorf("set QMI CAT CustomRaw mode: %w", err)
		}
	}

	claim, err := cat.ForceClaimEvents(ctx, uim.CATEventClaimConfig{
		RawMask:          profile.QMIEventMask(),
		FullFunctionMask: profile.QMIFullFunctionMask(),
	})
	if err != nil {
		return fmt.Errorf("claim QMI CAT events: %w", err)
	}
	if claim.ReleasedClientID != 0 {
		modem.Logger().Info(
			"claimed QMI CAT events",
			"clientID", claim.ClientID,
			"releasedClientID", claim.ReleasedClientID,
		)
	}
	return nil
}

func terminalProfile() stkpkg.Profile {
	return stkpkg.NewProfile(
		stkpkg.CapabilityProfileDownload,
		stkpkg.CapabilityBIP,
		stkpkg.CapabilitySetupEventList,
		stkpkg.CapabilityDisplayText,
		stkpkg.CapabilityGetInkey,
		stkpkg.CapabilityGetInput,
		stkpkg.CapabilitySetupMenu,
		stkpkg.CapabilityMenuSelection,
		stkpkg.CapabilitySelectItem,
		stkpkg.CapabilitySendSMS,
		stkpkg.CapabilitySendSS,
		stkpkg.CapabilitySendUSSD,
		stkpkg.CapabilitySendDTMF,
		stkpkg.CapabilitySetupCall,
		stkpkg.CapabilityLaunchBrowser,
	)
}
