package internet

type ConnectRequest struct {
	APN          string `json:"apn" validate:"omitempty,max=100"`
	DefaultRoute bool   `json:"defaultRoute"`
}

type ConnectionResponse struct {
	Status          string   `json:"status"`
	APN             string   `json:"apn"`
	DefaultRoute    bool     `json:"defaultRoute"`
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
