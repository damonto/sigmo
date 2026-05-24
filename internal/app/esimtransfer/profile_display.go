//go:build esim_transfer

package esimtransfer

import (
	"encoding/base64"
	"fmt"

	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/carrier"
)

func profileDisplayName(info *sgp22.ProfileInfo) string {
	if info.ProfileNickname != "" {
		return info.ProfileNickname
	}
	return info.ProfileName
}

func profileRegion(info *sgp22.ProfileInfo) string {
	carrierInfo := carrier.Lookup(info.ProfileOwner.MCC() + info.ProfileOwner.MNC())
	return carrierInfo.Region
}

func profileIconDataURL(icon sgp22.ProfileIcon) string {
	fileType := icon.FileType()
	if fileType == "" {
		return ""
	}
	return fmt.Sprintf("data:%s;base64,%s", fileType, base64.StdEncoding.EncodeToString(icon))
}

func profilePreviewFrom(info *sgp22.ProfileInfo) downloadProfilePreview {
	return downloadProfilePreview{
		ICCID:               info.ICCID.String(),
		ServiceProviderName: info.ServiceProviderName,
		ProfileName:         info.ProfileName,
		ProfileNickname:     info.ProfileNickname,
		ProfileState:        info.ProfileState.String(),
		Icon:                profileIconDataURL(info.Icon),
		RegionCode:          profileRegion(info),
	}
}
