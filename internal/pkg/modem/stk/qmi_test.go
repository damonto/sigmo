package stk

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/damonto/uicc-go/qcom/uim"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type fakeQMICATSession struct {
	config      uim.CATConfiguration
	setCalls    int
	setConfig   uim.CATConfiguration
	claimCalls  int
	claimConfig uim.CATEventClaimConfig
	claim       uim.CATEventClaim
	claimErr    error
}

func (s *fakeQMICATSession) Configuration(context.Context) (uim.CATConfiguration, error) {
	return s.config, nil
}

func (s *fakeQMICATSession) SetConfiguration(_ context.Context, config uim.CATConfiguration) error {
	s.setCalls++
	s.setConfig = config
	return nil
}

func (s *fakeQMICATSession) ForceClaimEvents(_ context.Context, config uim.CATEventClaimConfig) (uim.CATEventClaim, error) {
	s.claimCalls++
	s.claimConfig = config
	return s.claim, s.claimErr
}

func TestEnsureCATReady(t *testing.T) {
	tests := []struct {
		name           string
		cat            *fakeQMICATSession
		wantSetCalls   int
		wantClaimCalls int
		wantErr        bool
	}{
		{
			name: "custom raw claims events without setting configuration",
			cat: &fakeQMICATSession{config: uim.CATConfiguration{
				Mode:          uim.CATConfigCustomRaw,
				CustomProfile: terminalProfile().Bytes(),
			}},
			wantClaimCalls: 1,
		},
		{
			name: "custom raw with stale profile updates and claims events",
			cat: &fakeQMICATSession{config: uim.CATConfiguration{
				Mode:          uim.CATConfigCustomRaw,
				CustomProfile: []byte{0x01},
			}},
			wantSetCalls:   1,
			wantClaimCalls: 1,
		},
		{
			name:           "non custom raw sets mode and claims events",
			cat:            &fakeQMICATSession{config: uim.CATConfiguration{Mode: uim.CATConfigGobi}},
			wantSetCalls:   1,
			wantClaimCalls: 1,
		},
		{
			name: "claim error is returned",
			cat: &fakeQMICATSession{
				config: uim.CATConfiguration{
					Mode:          uim.CATConfigCustomRaw,
					CustomProfile: terminalProfile().Bytes(),
				},
				claimErr: errors.New("claim rejected"),
			},
			wantClaimCalls: 1,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := terminalProfile()
			err := ensureCATReady(context.Background(), &mmodem.Modem{}, tt.cat)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ensureCATReady() error = nil, want non-nil")
				}
			} else if err != nil {
				t.Fatalf("ensureCATReady() error = %v", err)
			}
			if tt.cat.setCalls != tt.wantSetCalls {
				t.Fatalf("SetConfiguration calls = %d, want %d", tt.cat.setCalls, tt.wantSetCalls)
			}
			if tt.wantSetCalls > 0 {
				if tt.cat.setConfig.Mode != uim.CATConfigCustomRaw {
					t.Fatalf("SetConfiguration mode = %v, want CustomRaw", tt.cat.setConfig.Mode)
				}
				if !slices.Equal(tt.cat.setConfig.CustomProfile, profile.Bytes()) {
					t.Fatalf("SetConfiguration profile = % X, want STK profile", tt.cat.setConfig.CustomProfile)
				}
			}
			if tt.cat.claimCalls != tt.wantClaimCalls {
				t.Fatalf("ForceClaimEvents calls = %d, want %d", tt.cat.claimCalls, tt.wantClaimCalls)
			}
			if tt.wantClaimCalls > 0 {
				if tt.cat.claimConfig.RawMask != profile.QMIEventMask() {
					t.Fatalf("claim raw mask = 0x%08X, want 0x%08X", tt.cat.claimConfig.RawMask, profile.QMIEventMask())
				}
				if tt.cat.claimConfig.FullFunctionMask != profile.QMIFullFunctionMask() {
					t.Fatalf("claim full-function mask = 0x%08X, want 0x%08X", tt.cat.claimConfig.FullFunctionMask, profile.QMIFullFunctionMask())
				}
			}
		})
	}
}
