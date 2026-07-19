package euicc

import "context"

func (h *Handler) SecureElements(ctx context.Context, modemID string) (*SEsResponse, error) {
	device, err := h.registry.Find(ctx, modemID)
	if err != nil {
		return nil, err
	}
	return h.euicc.Get(device)
}
