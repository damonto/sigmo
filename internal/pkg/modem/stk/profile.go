package stk

import (
	stkpkg "github.com/damonto/uicc-go/usim/stk"

	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
)

func terminalCATProfile() mdevice.CATProfile {
	profile := terminalProfile()
	return mdevice.CATProfile{
		Data:             profile.Bytes(),
		EventMask:        profile.QMIEventMask(),
		FullFunctionMask: profile.QMIFullFunctionMask(),
	}
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
