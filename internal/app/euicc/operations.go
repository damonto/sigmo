package euicc

import "context"

func (s *Service) SecureElements(ctx context.Context, modemID string) (*SEsResponse, error) {
	device, err := s.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return s.euicc.Get(ctx, device)
}
