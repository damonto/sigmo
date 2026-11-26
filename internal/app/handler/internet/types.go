package internet

import (
	"encoding/json"
	"errors"
	"time"

	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
)

type ConnectRequest struct {
	APN          string `json:"apn" validate:"omitempty,max=100"`
	IPType       string `json:"ipType" validate:"omitempty,max=20"`
	APNUsername  string `json:"apnUsername" validate:"omitempty,max=100"`
	APNPassword  string `json:"apnPassword" validate:"omitempty,max=100"`
	APNAuth      string `json:"apnAuth" validate:"omitempty,max=50"`
	DefaultRoute bool   `json:"defaultRoute"`
	ProxyEnabled bool   `json:"proxyEnabled"`
	AlwaysOn     bool   `json:"alwaysOn"`
}

type UpdatePreferencesRequest struct {
	DefaultRoute bool `json:"defaultRoute"`
	ProxyEnabled bool `json:"proxyEnabled"`
	AlwaysOn     bool `json:"alwaysOn"`
}

type ConnectionResponse struct {
	Status          string   `json:"status" jsonschema:"connection state: connected or disconnected"`
	SessionID       string   `json:"sessionId" jsonschema:"identifier of the tracked Internet connection session; empty while disconnected"`
	APN             string   `json:"apn" jsonschema:"access point name used by the mobile data connection"`
	IPType          string   `json:"ipType" jsonschema:"requested bearer IP family, such as ipv4, ipv6, or ipv4v6"`
	APNUsername     string   `json:"apnUsername" jsonschema:"username configured for APN authentication; empty when unused"`
	APNPassword     string   `json:"apnPassword" jsonschema:"password configured for APN authentication; MCP responses redact this value"`
	APNAuth         string   `json:"apnAuth" jsonschema:"APN authentication method; empty when automatic or unused"`
	DefaultRoute    bool     `json:"defaultRoute" jsonschema:"whether this connection installs the system default route"`
	ProxyEnabled    bool     `json:"proxyEnabled" jsonschema:"whether Sigmo's local HTTP and SOCKS5 proxy is enabled for this connection"`
	AlwaysOn        bool     `json:"alwaysOn" jsonschema:"whether Sigmo should automatically restore this connection"`
	Proxy           Proxy    `json:"proxy" jsonschema:"local proxy status and endpoints; MCP responses redact the password"`
	InterfaceName   string   `json:"interfaceName" jsonschema:"Linux network interface carrying the connection; empty while disconnected"`
	Bearer          uint64   `json:"bearer" jsonschema:"process-owned modem bearer identifier; zero while disconnected"`
	IPv4Addresses   []string `json:"ipv4Addresses" jsonschema:"assigned IPv4 addresses in CIDR notation"`
	IPv6Addresses   []string `json:"ipv6Addresses" jsonschema:"assigned IPv6 addresses in CIDR notation"`
	DNS             []string `json:"dns" jsonschema:"DNS server addresses assigned to the connection"`
	DurationSeconds uint32   `json:"durationSeconds" jsonschema:"elapsed connection duration in seconds"`
	TXBytes         uint64   `json:"txBytes" jsonschema:"bytes transmitted through this connection"`
	RXBytes         uint64   `json:"rxBytes" jsonschema:"bytes received through this connection"`
	RouteMetric     int      `json:"routeMetric" jsonschema:"metric used for routes installed by this connection"`
}

// TrafficResponse contains the connection state and an optional fresh sample.
type TrafficResponse struct {
	Connected bool           `json:"connected"`
	SessionID string         `json:"sessionId"`
	Sample    *TrafficSample `json:"sample"`
}

// TrafficSample contains session byte totals and their monotonic sample time.
type TrafficSample struct {
	ElapsedMilliseconds int64  `json:"elapsedMilliseconds"`
	RXBytes             uint64 `json:"rxBytes"`
	TXBytes             uint64 `json:"txBytes"`
}

type PublicResponse struct {
	IP           string `json:"ip" jsonschema:"public IP address observed for this modem connection"`
	Country      string `json:"country" jsonschema:"country associated with the public IP; empty when unavailable"`
	Organization string `json:"organization" jsonschema:"network organization associated with the public IP; empty when unavailable"`
	ISP          string `json:"isp,omitempty" jsonschema:"internet service provider associated with the public IP"`
}

// SpeedTestRequest identifies the target SIM and optionally selects the payload size.
type SpeedTestRequest struct {
	ICCID  string          `json:"iccid" validate:"required,max=64"`
	SizeMB json.RawMessage `json:"sizeMB,omitempty"`
}

func (req SpeedTestRequest) size() (internetcore.SpeedTestSize, error) {
	// Decode the size separately so a malformed size gets its own request error.
	var sizeMB *internetcore.SpeedTestSize
	if len(req.SizeMB) > 0 {
		if err := json.Unmarshal(req.SizeMB, &sizeMB); err != nil {
			return 0, errors.New("speed test size must be an integer")
		}
	}
	if sizeMB == nil {
		return internetcore.DefaultSpeedTestSize, nil
	}
	return *sizeMB, sizeMB.Validate()
}

// SpeedTestResponse contains the latest successful test for the current SIM.
type SpeedTestResponse struct {
	ICCID  string           `json:"iccid"`
	Result *SpeedTestResult `json:"result"`
}

// SpeedTestResult contains transfer rates in bits per second and the UTC completion time.
type SpeedTestResult struct {
	DownloadBPS int64     `json:"downloadBps"`
	UploadBPS   int64     `json:"uploadBps"`
	TestedAt    time.Time `json:"testedAt"`
}

type Proxy struct {
	Enabled       bool   `json:"enabled" jsonschema:"whether the local proxy is running"`
	Username      string `json:"username" jsonschema:"username required by the local proxy; empty when the proxy is disabled"`
	Password      string `json:"password" jsonschema:"password required by the local proxy; MCP responses redact this value"`
	HTTPAddress   string `json:"httpAddress" jsonschema:"local HTTP proxy listen address; empty when unavailable"`
	SOCKS5Address string `json:"socks5Address" jsonschema:"local SOCKS5 proxy listen address; empty when unavailable"`
}
