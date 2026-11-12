package modem

import "context"

func (s *Service) ListModems(ctx context.Context) ([]*ModemResponse, error) {
	return s.catalog.List(ctx)
}

func (s *Service) Modem(ctx context.Context, id string) (*ModemResponse, error) {
	device, err := s.registry.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.catalog.Get(ctx, device)
}

func (s *Service) SwitchSIM(ctx context.Context, id string, slot uint32) error {
	device, err := s.registry.Find(ctx, id)
	if err != nil {
		return err
	}
	return s.switchSIM(ctx, device, slot)
}

func (s *Service) UpdateNumber(ctx context.Context, id string, number string) error {
	device, err := s.registry.Find(ctx, id)
	if err != nil {
		return err
	}
	return s.updateNumber(ctx, device, number)
}
