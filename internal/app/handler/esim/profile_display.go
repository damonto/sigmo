package esim

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

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

func profileResponseFrom(info *sgp22.ProfileInfo) ProfileResponse {
	return ProfileResponse{
		Name:                profileDisplayName(info),
		ServiceProviderName: info.ServiceProviderName,
		ICCID:               info.ICCID.String(),
		ISDPAID:             info.ISDPAID.String(),
		Icon:                profileIconDataURL(info.Icon),
		ProfileName:         info.ProfileName,
		ProfileNickname:     info.ProfileNickname,
		ProfileState:        uint8(info.ProfileState),
		ProfileStateName:    info.ProfileState.String(),
		ProfileClass:        info.ProfileClass.String(),
		ProfileOwner:        profileOwnerResponse(info.ProfileOwner),
		RegionCode:          profileRegion(info),
	}
}

func profileOwnerResponse(owner sgp22.OperatorId) ProfileOwnerResponse {
	return ProfileOwnerResponse{
		MCC:  owner.MCC(),
		MNC:  owner.MNC(),
		GID1: strings.ToUpper(hex.EncodeToString(owner.GID1)),
		GID2: strings.ToUpper(hex.EncodeToString(owner.GID2)),
	}
}

func profilePreviewFrom(info *sgp22.ProfileInfo) downloadProfilePreview {
	return downloadProfilePreview{
		ICCID:               info.ICCID.String(),
		ServiceProviderName: info.ServiceProviderName,
		ProfileName:         info.ProfileName,
		ProfileNickname:     info.ProfileNickname,
		ProfileState:        info.ProfileState.String(),
		ProfileOwner:        profileOwnerResponse(info.ProfileOwner),
		Icon:                profileIconDataURL(info.Icon),
		RegionCode:          profileRegion(info),
	}
}
