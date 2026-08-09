package network

import (
	"fmt"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

var radioTechnologies = []struct {
	value wwanmodem.Technology
	label string
}{
	{wwanmodem.TechnologyGSM, "GSM"},
	{wwanmodem.TechnologyUMTS, "UMTS"},
	{wwanmodem.TechnologyLTE, "LTE"},
	{wwanmodem.TechnologyLTECatM, "LTE Cat-M"},
	{wwanmodem.TechnologyLTENB, "LTE NB-IoT"},
	{wwanmodem.TechnologyNR5GNSA, "5G NSA"},
	{wwanmodem.TechnologyNR5GSA, "5G SA"},
}

func technologyLabel(technology wwanmodem.Technology) string {
	if technology == 0 {
		return "None"
	}
	parts := make([]string, 0, len(radioTechnologies))
	for _, candidate := range radioTechnologies {
		if technology&candidate.value != 0 {
			parts = append(parts, candidate.label)
		}
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	return strings.Join(parts, " + ")
}

func bandLabel(band wwanmodem.Band) string {
	switch band.Technology {
	case wwanmodem.TechnologyGSM:
		return fmt.Sprintf("GSM %d", band.Number)
	case wwanmodem.TechnologyUMTS:
		return fmt.Sprintf("UMTS B%d", band.Number)
	case wwanmodem.TechnologyLTE:
		return fmt.Sprintf("LTE B%d", band.Number)
	case wwanmodem.TechnologyLTECatM:
		return fmt.Sprintf("LTE Cat-M B%d", band.Number)
	case wwanmodem.TechnologyLTENB:
		return fmt.Sprintf("LTE NB-IoT B%d", band.Number)
	case wwanmodem.TechnologyNR5GNSA,
		wwanmodem.TechnologyNR5GSA,
		wwanmodem.TechnologyNR5GNSA | wwanmodem.TechnologyNR5GSA:
		return fmt.Sprintf("NR n%d", band.Number)
	default:
		return fmt.Sprintf("Band %d", band.Number)
	}
}

func networkAvailabilityName(operator wwanmodem.Operator) string {
	switch {
	case operator.Current:
		return "Current"
	case operator.Forbidden:
		return "Forbidden"
	case operator.Available:
		return "Available"
	default:
		return "Unknown"
	}
}

func accessTechnologyStrings(technology wwanmodem.Technology) []string {
	names := make([]string, 0, len(radioTechnologies))
	for _, candidate := range radioTechnologies {
		if technology&candidate.value != 0 {
			names = append(names, candidate.label)
		}
	}
	return names
}
