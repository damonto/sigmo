package internet

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

type ConnectionResponse struct {
	Status          string   `json:"status"`
	APN             string   `json:"apn"`
	IPType          string   `json:"ipType"`
	APNUsername     string   `json:"apnUsername"`
	APNPassword     string   `json:"apnPassword"`
	APNAuth         string   `json:"apnAuth"`
	DefaultRoute    bool     `json:"defaultRoute"`
	ProxyEnabled    bool     `json:"proxyEnabled"`
	AlwaysOn        bool     `json:"alwaysOn"`
	Proxy           Proxy    `json:"proxy"`
	InterfaceName   string   `json:"interfaceName,omitempty"`
	Bearer          string   `json:"bearer,omitempty"`
	IPv4Addresses   []string `json:"ipv4Addresses"`
	IPv6Addresses   []string `json:"ipv6Addresses"`
	DNS             []string `json:"dns"`
	DurationSeconds uint32   `json:"durationSeconds"`
	TXBytes         uint64   `json:"txBytes"`
	RXBytes         uint64   `json:"rxBytes"`
	RouteMetric     int      `json:"routeMetric"`
}

type PublicResponse struct {
	IP           string `json:"ip,omitempty"`
	Country      string `json:"country,omitempty"`
	Organization string `json:"organization,omitempty"`
}

type Proxy struct {
	Enabled       bool   `json:"enabled"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	HTTPAddress   string `json:"httpAddress,omitempty"`
	SOCKS5Address string `json:"socks5Address,omitempty"`
}
