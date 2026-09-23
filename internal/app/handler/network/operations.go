package network

import "context"

func (h *Handler) ListNetworks(ctx context.Context, modemID string) ([]NetworkResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.networks.List(ctx, device)
}

func (h *Handler) SetNetworkRegistration(ctx context.Context, modemID string, req SetRegistrationRequest) error {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	return h.networks.SetRegistration(ctx, device, req)
}

func (h *Handler) NetworkRegistration(ctx context.Context, modemID string) (*RegistrationResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.networks.Registration(ctx, device)
}

func (h *Handler) NetworkModes(ctx context.Context, modemID string) (*ModesResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.networks.Modes(ctx, device)
}

func (h *Handler) NetworkBands(ctx context.Context, modemID string) (*BandsResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.networks.Bands(ctx, device)
}

func (h *Handler) AirplaneModeValue(ctx context.Context, modemID string) (*AirplaneModeResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.networks.AirplaneMode(ctx, device)
}

func (h *Handler) UpdateAirplaneMode(ctx context.Context, modemID string, enabled bool) error {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	return h.networks.SetAirplaneMode(ctx, device, SetAirplaneModeRequest{Enabled: enabled})
}
