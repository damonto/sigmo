package modem

import "fmt"

type ModemState int32

const (
	ModemStateFailed        ModemState = iota - 1 // The modem is unusable.
	ModemStateUnknown                             // State unknown or not reportable.
	ModemStateInitializing                        // The modem is currently being initialized.
	ModemStateLocked                              // The modem needs to be unlocked.
	ModemStateDisabled                            // The modem is not enabled and is powered down.
	ModemStateDisabling                           // The modem is currently transitioning to the @ModemStateDisabled state.
	ModemStateEnabling                            // The modem is currently transitioning to the @ModemStateEnabled state.
	ModemStateEnabled                             // The modem is enabled and powered on but not registered with a network provider and not available for data connections.
	ModemStateSearching                           // The modem is searching for a network provider to register with.
	ModemStateRegistered                          // The modem is registered with a network provider, and data connections and messaging may be available for use.
	ModemStateDisconnecting                       // The modem is disconnecting and deactivating the last active packet data bearer. This state will not be entered if more than one packet data bearer is active and one of the active bearers is deactivated.
	ModemStateConnecting                          // The modem is activating and connecting the first packet data bearer. Subsequent bearer activations when another bearer is already active do not cause this state to be entered.
	ModemStateConnected                           // One or more packet data bearers is active and connected.
)

type ModemPortType uint32

const (
	ModemPortTypeUnknown ModemPortType = iota + 1 // Unknown.
	ModemPortTypeNet                              // Net port.
	ModemPortTypeAt                               // AT port.
	ModemPortTypeQcdm                             // QCDM port.
	ModemPortTypeGps                              // GPS port.
	ModemPortTypeQmi                              // QMI port.
	ModemPortTypeMbim                             // MBIM port.
	ModemPortTypeAudio                            // Audio port.
)

type SMSState uint32

const (
	SMSStateUnknown   SMSState = iota // State unknown or not reportable.
	SMSStateStored                    // The message has been neither received nor yet sent.
	SMSStateReceiving                 // The message is being received but is not yet complete.
	SMSStateReceived                  // The message has been completely received.
	SMSStateSending                   // The message is queued for delivery.
	SMSStateSent                      // The message was successfully sent.
)

func (s SMSState) String() string {
	switch s {
	case SMSStateUnknown:
		return "Unknown"
	case SMSStateStored:
		return "Stored"
	case SMSStateReceiving:
		return "Receiving"
	case SMSStateReceived:
		return "Received"
	case SMSStateSending:
		return "Sending"
	case SMSStateSent:
		return "Sent"
	default:
		return "Unknown"
	}
}

type SMSStorage uint32

const (
	SMSStorageUnknown SMSStorage = iota // Storage unknown.
	SMSStorageSM                        // SIM card storage area.
	SMSStorageME                        // Mobile equipment storage area.
	SMSStorageMT                        // Sum of SIM and Mobile equipment storages.
	SMSStorageSR                        // Status report message storage area.
	SMSStorageBM                        // Broadcast message storage area.
	SMSStorageTA                        // Terminal adaptor message storage area.
)

func (s SMSStorage) String() string {
	switch s {
	case SMSStorageUnknown:
		return "unknown"
	case SMSStorageSM:
		return "sm"
	case SMSStorageME:
		return "me"
	case SMSStorageMT:
		return "mt"
	case SMSStorageSR:
		return "sr"
	case SMSStorageBM:
		return "bm"
	case SMSStorageTA:
		return "ta"
	default:
		return "unknown"
	}
}

type Modem3gppRegistrationState uint32

const (
	Modem3gppRegistrationStateIdle                    Modem3gppRegistrationState = iota // Not registered, not searching for new operator to register.
	Modem3gppRegistrationStateHome                                                      // Registered on home network.
	Modem3gppRegistrationStateSearching                                                 // Not registered, searching for new operator to register with.
	Modem3gppRegistrationStateDenied                                                    // Registration denied.
	Modem3gppRegistrationStateUnknown                                                   // Unknown registration status.
	Modem3gppRegistrationStateRoaming                                                   // Registered on a roaming network.
	Modem3gppRegistrationStateHomeSmsOnly                                               // Registered for "SMS only", home network (applicable only when on LTE).
	Modem3gppRegistrationStateRoamingSmsOnly                                            // Registered for "SMS only", roaming network (applicable only when on LTE).
	Modem3gppRegistrationStateEmergencyOnly                                             // Emergency services only.
	Modem3gppRegistrationStateHomeCsfbNotPreferred                                      // Registered for "CSFB not preferred", home network (applicable only when on LTE).
	Modem3gppRegistrationStateRoamingCsfbNotPreferred                                   // Registered for "CSFB not preferred", roaming network (applicable only when on LTE).
)

func (m Modem3gppRegistrationState) String() string {
	switch m {
	case Modem3gppRegistrationStateIdle:
		return "Idle"
	case Modem3gppRegistrationStateHome:
		return "Home"
	case Modem3gppRegistrationStateSearching:
		return "Searching"
	case Modem3gppRegistrationStateDenied:
		return "Denied"
	case Modem3gppRegistrationStateUnknown:
		return "Unknown"
	case Modem3gppRegistrationStateRoaming:
		return "Roaming"
	case Modem3gppRegistrationStateHomeSmsOnly:
		return "Home SMS Only"
	case Modem3gppRegistrationStateRoamingSmsOnly:
		return "Roaming SMS Only"
	case Modem3gppRegistrationStateEmergencyOnly:
		return "Emergency Only"
	case Modem3gppRegistrationStateHomeCsfbNotPreferred:
		return "Home CSFB Not Preferred"
	case Modem3gppRegistrationStateRoamingCsfbNotPreferred:
		return "Roaming CSFB Not Preferred"
	default:
		return "Undefined"
	}
}

type Modem3gppUssdSessionState uint32

const (
	Modem3gppUssdSessionStateUnknown      Modem3gppUssdSessionState = iota // Unknown state.
	Modem3gppUssdSessionStateIdle                                          // No active session.
	Modem3gppUssdSessionStateActive                                        // A session is active and the mobile is waiting for a response.
	Modem3gppUssdSessionStateUserResponse                                  // The network is waiting for the client's response.
)

type ModemAccessTechnology uint32

const (
	ModemAccessTechnologyUnknown    ModemAccessTechnology = 0          // The access technology used is unknown.
	ModemAccessTechnologyPots       ModemAccessTechnology = 1 << 0     // Analog wireline telephone.
	ModemAccessTechnologyGsm        ModemAccessTechnology = 1 << 1     // GSM.
	ModemAccessTechnologyGsmCompact ModemAccessTechnology = 1 << 2     // Compact GSM.
	ModemAccessTechnologyGprs       ModemAccessTechnology = 1 << 3     // GPRS.
	ModemAccessTechnologyEdge       ModemAccessTechnology = 1 << 4     // EDGE (ETSI 27.007: "GSM w/EGPRS").
	ModemAccessTechnologyUmts       ModemAccessTechnology = 1 << 5     // UMTS (ETSI 27.007: "UTRAN").
	ModemAccessTechnologyHsdpa      ModemAccessTechnology = 1 << 6     // HSDPA (ETSI 27.007: "UTRAN w/HSDPA").
	ModemAccessTechnologyHsupa      ModemAccessTechnology = 1 << 7     // HSUPA (ETSI 27.007: "UTRAN w/HSUPA").
	ModemAccessTechnologyHspa       ModemAccessTechnology = 1 << 8     // HSPA (ETSI 27.007: "UTRAN w/HSDPA and HSUPA").
	ModemAccessTechnologyHspaPlus   ModemAccessTechnology = 1 << 9     // HSPA+ (ETSI 27.007: "UTRAN w/HSPA+").
	ModemAccessTechnology1xrtt      ModemAccessTechnology = 1 << 10    // CDMA2000 1xRTT.
	ModemAccessTechnologyEvdo0      ModemAccessTechnology = 1 << 11    // CDMA2000 EVDO revision 0.
	ModemAccessTechnologyEvdoa      ModemAccessTechnology = 1 << 12    // CDMA2000 EVDO revision A.
	ModemAccessTechnologyEvdob      ModemAccessTechnology = 1 << 13    // CDMA2000 EVDO revision B.
	ModemAccessTechnologyLte        ModemAccessTechnology = 1 << 14    // LTE (ETSI 27.007: "E-UTRAN")
	ModemAccessTechnology5GNR       ModemAccessTechnology = 1 << 15    // 5GNR (ETSI 27.007: "NG-RAN"). Since 1.14.
	ModemAccessTechnologyLteCatM    ModemAccessTechnology = 1 << 16    // Cat-M (ETSI 23.401: LTE Category M1/M2). Since 1.20.
	ModemAccessTechnologyLteNBIot   ModemAccessTechnology = 1 << 17    // NB IoT (ETSI 23.401: LTE Category NB1/NB2). Since 1.20.
	ModemAccessTechnologyAny        ModemAccessTechnology = 0xFFFFFFFF // Mask specifying all access technologies.
)

func (m ModemAccessTechnology) UnmarshalBitmask(bitmask uint32) []ModemAccessTechnology {
	if bitmask == 0 {
		return nil
	}
	if bitmask == uint32(ModemAccessTechnologyAny) {
		return []ModemAccessTechnology{ModemAccessTechnologyAny}
	}
	supported := []ModemAccessTechnology{
		ModemAccessTechnologyPots,
		ModemAccessTechnologyGsm,
		ModemAccessTechnologyGsmCompact,
		ModemAccessTechnologyGprs,
		ModemAccessTechnologyEdge,
		ModemAccessTechnologyUmts,
		ModemAccessTechnologyHsdpa,
		ModemAccessTechnologyHsupa,
		ModemAccessTechnologyHspa,
		ModemAccessTechnologyHspaPlus,
		ModemAccessTechnology1xrtt,
		ModemAccessTechnologyEvdo0,
		ModemAccessTechnologyEvdoa,
		ModemAccessTechnologyEvdob,
		ModemAccessTechnologyLte,
		ModemAccessTechnology5GNR,
		ModemAccessTechnologyLteCatM,
		ModemAccessTechnologyLteNBIot,
	}
	var accessTechnologies []ModemAccessTechnology
	for _, tech := range supported {
		if bitmask&uint32(tech) != 0 {
			accessTechnologies = append(accessTechnologies, tech)
		}
	}
	return accessTechnologies
}

func (m ModemAccessTechnology) String() string {
	switch m {
	case ModemAccessTechnologyUnknown:
		return "Unknown"
	case ModemAccessTechnologyPots:
		return "POTS"
	case ModemAccessTechnologyGsm:
		return "GSM"
	case ModemAccessTechnologyGsmCompact:
		return "GSM Compact"
	case ModemAccessTechnologyGprs:
		return "GPRS"
	case ModemAccessTechnologyEdge:
		return "EDGE"
	case ModemAccessTechnologyUmts:
		return "UMTS"
	case ModemAccessTechnologyHsdpa:
		return "HSDPA"
	case ModemAccessTechnologyHsupa:
		return "HSUPA"
	case ModemAccessTechnologyHspa:
		return "HSPA"
	case ModemAccessTechnologyHspaPlus:
		return "HSPA+"
	case ModemAccessTechnology1xrtt:
		return "CDMA2000 1xRTT"
	case ModemAccessTechnologyEvdo0:
		return "CDMA2000 EVDO revision 0"
	case ModemAccessTechnologyEvdoa:
		return "CDMA2000 EVDO revision A"
	case ModemAccessTechnologyEvdob:
		return "CDMA2000 EVDO revision B"
	case ModemAccessTechnologyLte:
		return "LTE"
	case ModemAccessTechnology5GNR:
		return "5GNR"
	case ModemAccessTechnologyLteCatM:
		return "LTE Cat-M"
	case ModemAccessTechnologyLteNBIot:
		return "LTE NB-IoT"
	case ModemAccessTechnologyAny:
		return "Any"
	default:
		return "Unknown"
	}
}

type ModemMode uint32

const (
	ModemModeNone ModemMode = 0
	ModemModeCS   ModemMode = 1 << 0
	ModemMode2G   ModemMode = 1 << 1
	ModemMode3G   ModemMode = 1 << 2
	ModemMode4G   ModemMode = 1 << 3
	ModemMode5G   ModemMode = 1 << 4
	ModemModeAny  ModemMode = 0xFFFFFFFF
)

type ModemModePair struct {
	Allowed   ModemMode
	Preferred ModemMode
}

func (m ModemMode) String() string {
	switch m {
	case ModemModeNone:
		return "None"
	case ModemModeCS:
		return "CS"
	case ModemMode2G:
		return "2G"
	case ModemMode3G:
		return "3G"
	case ModemMode4G:
		return "4G"
	case ModemMode5G:
		return "5G"
	case ModemModeAny:
		return "Any"
	default:
		return "Unknown"
	}
}

func (m ModemMode) Label() string {
	if m == ModemModeAny || m == ModemModeNone {
		return m.String()
	}
	parts := make([]string, 0, 5)
	for _, mode := range []ModemMode{ModemModeCS, ModemMode2G, ModemMode3G, ModemMode4G, ModemMode5G} {
		if m&mode != 0 {
			parts = append(parts, mode.String())
		}
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	label := parts[0]
	for _, part := range parts[1:] {
		label += " + " + part
	}
	return label
}

type ModemBand uint32

const (
	ModemBandUnknown ModemBand = 0
	ModemBandAny     ModemBand = 256
)

func (b ModemBand) String() string {
	switch b {
	case ModemBandUnknown:
		return "Unknown"
	case ModemBandAny:
		return "Any"
	}
	if label, ok := modemBandLabels[b]; ok {
		return label
	}
	if b >= 31 && b <= 115 {
		if b == 115 {
			return "LTE B85"
		}
		return fmt.Sprintf("LTE B%d", b-30)
	}
	if b >= 301 && b < 600 {
		return fmt.Sprintf("NR n%d", b-300)
	}
	return fmt.Sprintf("Band %d", b)
}

var modemBandLabels = map[ModemBand]string{
	1:   "GSM EGSM 900",
	2:   "GSM DCS 1800",
	3:   "GSM PCS 1900",
	4:   "GSM 850",
	5:   "UMTS band 1",
	6:   "UMTS band 3",
	7:   "UMTS band 4",
	8:   "UMTS band 6",
	9:   "UMTS band 5",
	10:  "UMTS band 8",
	11:  "UMTS band 9",
	12:  "UMTS band 2",
	13:  "UMTS band 7",
	14:  "GSM 450",
	15:  "GSM 480",
	16:  "GSM 750",
	17:  "GSM 380",
	18:  "GSM 410",
	19:  "GSM 710",
	20:  "GSM 810",
	128: "CDMA BC0",
	129: "CDMA BC1",
	130: "CDMA BC2",
	131: "CDMA BC3",
	132: "CDMA BC4",
	134: "CDMA BC5",
	135: "CDMA BC6",
	136: "CDMA BC7",
	137: "CDMA BC8",
	138: "CDMA BC9",
	139: "CDMA BC10",
	140: "CDMA BC11",
	141: "CDMA BC12",
	142: "CDMA BC13",
	143: "CDMA BC14",
	144: "CDMA BC15",
	145: "CDMA BC16",
	146: "CDMA BC17",
	147: "CDMA BC18",
	148: "CDMA BC19",
	210: "UMTS band 10",
	211: "UMTS band 11",
	212: "UMTS band 12",
	213: "UMTS band 13",
	214: "UMTS band 14",
	219: "UMTS band 19",
	220: "UMTS band 20",
	221: "UMTS band 21",
	222: "UMTS band 22",
	225: "UMTS band 25",
	226: "UMTS band 26",
	232: "UMTS band 32",
}

type CellType uint32

const (
	CellTypeUnknown CellType = iota
	CellTypeCDMA
	CellTypeGSM
	CellTypeUMTS
	CellTypeTDSCDMA
	CellTypeLTE
	CellType5GNR
)

func (c CellType) String() string {
	switch c {
	case CellTypeCDMA:
		return "CDMA"
	case CellTypeGSM:
		return "GSM"
	case CellTypeUMTS:
		return "UMTS"
	case CellTypeTDSCDMA:
		return "TDSCDMA"
	case CellTypeLTE:
		return "LTE"
	case CellType5GNR:
		return "5GNR"
	default:
		return "Unknown"
	}
}

type Modem3gppNetworkAvailability uint32

const (
	Modem3gppNetworkAvailabilityUnknown   Modem3gppNetworkAvailability = iota // Unknown.
	Modem3gppNetworkAvailabilityAvailable                                     // Network available.
	Modem3gppNetworkAvailabilityCurrent                                       // Network is the current one.
	Modem3gppNetworkAvailabilityForbidden                                     // Network is forbidden.
)

func (m Modem3gppNetworkAvailability) String() string {
	switch m {
	case Modem3gppNetworkAvailabilityUnknown:
		return "Unknown"
	case Modem3gppNetworkAvailabilityAvailable:
		return "Available"
	case Modem3gppNetworkAvailabilityCurrent:
		return "Current"
	case Modem3gppNetworkAvailabilityForbidden:
		return "Forbidden"
	default:
		return "Undefined"
	}
}
