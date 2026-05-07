package modem

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
