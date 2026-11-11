package esimtransfer

import "github.com/damonto/ts43-go"

// The versioned wire types contain only fields used by this protocol. Keeping
// them separate from TS.43 types makes dependency upgrades explicit at the API.
type peerDevice struct {
	IMEI            string `json:"imei"`
	ICCID           string `json:"iccid"`
	EID             string `json:"eid"`
	Vendor          string `json:"vendor"`
	Model           string `json:"model"`
	SoftwareVersion string `json:"softwareVersion"`
}

func peerDeviceFrom(device ts43.Device) peerDevice {
	return peerDevice{IMEI: device.IMEI, ICCID: device.ICCID, EID: device.EID,
		Vendor: device.Vendor, Model: device.Model, SoftwareVersion: device.SoftwareVersion}
}
func (device peerDevice) device() ts43.Device {
	return ts43.Device{IMEI: device.IMEI, ICCID: device.ICCID, EID: device.EID,
		Vendor: device.Vendor, Model: device.Model, SoftwareVersion: device.SoftwareVersion}
}

type peerIdentity struct {
	ICCID        string `json:"iccid"`
	IMSI         string `json:"imsi"`
	MCC          string `json:"mcc"`
	MNC          string `json:"mnc"`
	GID1         string `json:"gid1,omitempty"`
	GID2         string `json:"gid2,omitempty"`
	SPN          string `json:"spn,omitempty"`
	PreferredAPN string `json:"preferredApn,omitempty"`
}

func peerIdentityFrom(identity *ts43.Identity) *peerIdentity {
	if identity == nil {
		return nil
	}
	return &peerIdentity{ICCID: identity.ICCID, IMSI: identity.IMSI, MCC: identity.MCC, MNC: identity.MNC,
		GID1: identity.GID1, GID2: identity.GID2, SPN: identity.SPN, PreferredAPN: identity.PreferredAPN}
}
func (identity peerIdentity) identity() *ts43.Identity {
	return &ts43.Identity{ICCID: identity.ICCID, IMSI: identity.IMSI, MCC: identity.MCC, MNC: identity.MNC,
		GID1: identity.GID1, GID2: identity.GID2, SPN: identity.SPN, PreferredAPN: identity.PreferredAPN}
}

type peerAKARequest struct {
	Rand []byte `json:"rand"`
	Autn []byte `json:"autn"`
}

func (request peerAKARequest) request() *ts43.AKARequest {
	return &ts43.AKARequest{Rand: request.Rand, Autn: request.Autn}
}

type peerAKAResponse struct {
	Res  []byte `json:"res,omitempty"`
	CK   []byte `json:"ck,omitempty"`
	IK   []byte `json:"ik,omitempty"`
	Auts []byte `json:"auts,omitempty"`
}

func peerAKAResponseFrom(response *ts43.AKAResponse) *peerAKAResponse {
	if response == nil {
		return nil
	}
	return &peerAKAResponse{Res: response.Res, CK: response.CK, IK: response.IK, Auts: response.Auts}
}
func (response peerAKAResponse) response() *ts43.AKAResponse {
	return &ts43.AKAResponse{Res: response.Res, CK: response.CK, IK: response.IK, Auts: response.Auts}
}
