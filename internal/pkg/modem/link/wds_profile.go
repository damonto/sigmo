package link

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/wwan-go/qcom"
)

func wdsProfileIndex(ctx context.Context, client *qcom.Client, apn string, preference qcom.WDSIPPreference) (uint8, error) {
	settings, err := wdsProfileSettings(ctx, client)
	if err != nil {
		return 0, err
	}
	return selectWDSProfileIndex(apn, preference, settings)
}

func wdsDualStackProfileIndex(ctx context.Context, client *qcom.Client, apn string) (uint8, error) {
	settings, err := wdsProfileSettings(ctx, client)
	if err != nil {
		return 0, err
	}
	return selectWDSDualStackProfileIndex(apn, settings)
}

func wdsProfileSettings(ctx context.Context, client *qcom.Client) ([]qcom.WDSProfileSettings, error) {
	profiles, err := client.WDSProfiles(ctx, qcom.WDSProfileType3GPP)
	if err != nil {
		return nil, err
	}
	settings := make([]qcom.WDSProfileSettings, 0, len(profiles))
	for _, profile := range profiles {
		profileSettings, err := client.WDSProfileSettings(ctx, profile.ID)
		if err != nil {
			return nil, err
		}
		settings = append(settings, profileSettings)
	}
	return settings, nil
}

func selectWDSProfileIndex(apn string, preference qcom.WDSIPPreference, profiles []qcom.WDSProfileSettings) (uint8, error) {
	apn = strings.TrimSpace(apn)
	if apn == "" {
		return 0, errors.New("WDS APN is required")
	}

	var want qcom.WDSPDPType
	switch preference {
	case qcom.WDSIPPreferenceIPv4:
		want = qcom.WDSPDPTypeIPv4
	case qcom.WDSIPPreferenceIPv6:
		want = qcom.WDSPDPTypeIPv6
	default:
		return 0, fmt.Errorf("unsupported WDS IP preference %d", preference)
	}

	var compatible uint8
	for _, profile := range profiles {
		if !profile.APNKnown || !strings.EqualFold(strings.TrimSpace(profile.APN), apn) || !profile.PDPKnown {
			continue
		}
		if profile.PDPType == want {
			return profile.ID.Index, nil
		}
		if profile.PDPType == qcom.WDSPDPTypeIPv4v6 && compatible == 0 {
			compatible = profile.ID.Index
		}
	}
	if compatible != 0 {
		return compatible, nil
	}
	return 0, fmt.Errorf("%w: APN %q with IP preference %d", qcom.ErrWDSProfileNotFound, apn, preference)
}

func selectWDSDualStackProfileIndex(apn string, profiles []qcom.WDSProfileSettings) (uint8, error) {
	apn = strings.TrimSpace(apn)
	if apn == "" {
		return 0, errors.New("WDS APN is required")
	}
	for _, profile := range profiles {
		if !profile.APNKnown || !strings.EqualFold(strings.TrimSpace(profile.APN), apn) || !profile.PDPKnown {
			continue
		}
		if profile.PDPType == qcom.WDSPDPTypeIPv4v6 {
			return profile.ID.Index, nil
		}
	}
	return 0, fmt.Errorf("%w: APN %q with dual-stack PDP type", qcom.ErrWDSProfileNotFound, apn)
}
