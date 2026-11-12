package network

import "context"

func (s *Service) ListNetworks(ctx context.Context, modemID string) ([]NetworkResponse, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.networks.List(ctx, device)
}

func (s *Service) SetNetworkRegistration(ctx context.Context, modemID string, req SetRegistrationRequest) error {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	return s.networks.SetRegistration(ctx, device, req)
}

func (s *Service) NetworkRegistration(ctx context.Context, modemID string) (*RegistrationResponse, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.networks.Registration(ctx, device)
}

func (s *Service) NetworkModes(ctx context.Context, modemID string) (*ModesResponse, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.networks.Modes(ctx, device)
}

func (s *Service) NetworkBands(ctx context.Context, modemID string) (*BandsResponse, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.networks.Bands(ctx, device)
}

func (s *Service) AirplaneModeValue(ctx context.Context, modemID string) (*AirplaneModeResponse, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.networks.AirplaneMode(ctx, device)
}

func (s *Service) UpdateAirplaneMode(ctx context.Context, modemID string, enabled bool) error {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return err
	}
	return s.networks.SetAirplaneMode(ctx, device, SetAirplaneModeRequest{Enabled: enabled})
}
