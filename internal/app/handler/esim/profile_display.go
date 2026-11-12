package esim

import (
	sgp22 "github.com/damonto/euicc-go/v2"

	appesim "github.com/damonto/sigmo/internal/app/esim"
)

func profilePreviewFrom(info *sgp22.ProfileInfo) downloadProfilePreview {
	profile := appesim.DescribeProfile(info)
	return downloadProfilePreview{
		ICCID:               info.ICCID.String(),
		ServiceProviderName: info.ServiceProviderName,
		ProfileName:         info.ProfileName,
		ProfileNickname:     info.ProfileNickname,
		ProfileState:        info.ProfileState.String(),
		ProfileOwner:        profile.ProfileOwner,
		Icon:                profile.Icon,
		RegionCode:          profile.RegionCode,
	}
}
