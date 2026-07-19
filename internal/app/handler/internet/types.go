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

type UpdatePreferencesRequest struct {
	DefaultRoute bool `json:"defaultRoute"`
	ProxyEnabled bool `json:"proxyEnabled"`
	AlwaysOn     bool `json:"alwaysOn"`
}

type ConnectionResponse struct {
	Status          string   `json:"status" jsonschema:"connection state: connected or disconnected"`
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
	Bearer          string   `json:"bearer" jsonschema:"modem bearer object identifier; empty while disconnected"`
	IPv4Addresses   []string `json:"ipv4Addresses" jsonschema:"assigned IPv4 addresses in CIDR notation"`
	IPv6Addresses   []string `json:"ipv6Addresses" jsonschema:"assigned IPv6 addresses in CIDR notation"`
	DNS             []string `json:"dns" jsonschema:"DNS server addresses assigned to the connection"`
	DurationSeconds uint32   `json:"durationSeconds" jsonschema:"elapsed connection duration in seconds"`
	TXBytes         uint64   `json:"txBytes" jsonschema:"bytes transmitted through this connection"`
	RXBytes         uint64   `json:"rxBytes" jsonschema:"bytes received through this connection"`
	RouteMetric     int      `json:"routeMetric" jsonschema:"metric used for routes installed by this connection"`
}

type PublicResponse struct {
	IP           string `json:"ip" jsonschema:"public IP address observed for this modem connection"`
	Country      string `json:"country" jsonschema:"country associated with the public IP; empty when unavailable"`
	Organization string `json:"organization" jsonschema:"network organization associated with the public IP; empty when unavailable"`
}

type Proxy struct {
	Enabled       bool   `json:"enabled" jsonschema:"whether the local proxy is running"`
	Username      string `json:"username" jsonschema:"username required by the local proxy; empty when the proxy is disabled"`
	Password      string `json:"password" jsonschema:"password required by the local proxy; MCP responses redact this value"`
	HTTPAddress   string `json:"httpAddress" jsonschema:"local HTTP proxy listen address; empty when unavailable"`
	SOCKS5Address string `json:"socks5Address" jsonschema:"local SOCKS5 proxy listen address; empty when unavailable"`
}
