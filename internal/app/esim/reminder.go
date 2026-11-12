package esim

import "strings"

// ProfileName resolves the display name of a profile within one secure element.
func ProfileName(profiles *Inventory, seID, iccid string) (string, bool) {
	if profiles == nil {
		return "", false
	}
	for _, group := range profiles.SEs {
		if strings.TrimSpace(group.ID) != strings.TrimSpace(seID) {
			continue
		}
		for _, profile := range group.Profiles {
			if profile.ICCID == iccid {
				return profile.Name, true
			}
		}
	}
	return "", false
}
