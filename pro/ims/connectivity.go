//go:build ims

package ims

import (
	"context"
	"errors"
	"strings"
	"sync"

	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/pro/websheet"
)

// Connectivity is the application entry point for Internet, Wi-Fi Calling,
// and VoLTE. Each access keeps its own settings and sessions; Connectivity
// owns cross-access policy and serializes configuration changes per modem.
type Connectivity struct {
	registry         *mmodem.Registry
	internet         *pinternet.Connector
	wifiCalling      *coordinator
	volte            *coordinator
	wifiCallingVoice *WiFiCalling
	volteVoice       *VoLTE
	reloadModem      func(context.Context, *mmodem.Modem) (*mmodem.Modem, error)

	operationMu sync.Mutex
	operations  map[string]*sync.Mutex
}

type ConnectivityConfig struct {
	Store              *storage.Store
	Registry           *mmodem.Registry
	Internet           *pinternet.Connector
	NetworkPreferences airplaneModePreferences
	OnIncoming         IncomingSMSFunc
	Websheets          *websheet.Broker
}

func NewConnectivity(cfg ConnectivityConfig) *Connectivity {
	registrationGroups := &RegistrationGroups{}
	wifiCalling := newCoordinator(coordinatorConfig{
		Store:              cfg.Store,
		Registry:           cfg.Registry,
		Access:             AccessWiFiCalling,
		Internet:           cfg.Internet,
		RegistrationGroups: registrationGroups,
		OnIncoming:         cfg.OnIncoming,
		Websheets:          cfg.Websheets,
	})
	volte := newCoordinator(coordinatorConfig{
		Store:              cfg.Store,
		Registry:           cfg.Registry,
		Access:             AccessVoLTE,
		Internet:           cfg.Internet,
		NetworkPreferences: cfg.NetworkPreferences,
		RegistrationGroups: registrationGroups,
		OnIncoming:         cfg.OnIncoming,
	})
	connectivity := &Connectivity{
		registry:         cfg.Registry,
		internet:         cfg.Internet,
		wifiCalling:      wifiCalling,
		volte:            volte,
		wifiCallingVoice: &WiFiCalling{voiceAccess: &voiceAccess{access: wifiCalling}},
		volteVoice:       &VoLTE{voiceAccess: &voiceAccess{access: volte}},
		operations:       make(map[string]*sync.Mutex),
	}
	if cfg.Registry != nil {
		connectivity.reloadModem = cfg.Registry.Reload
	}
	return connectivity
}

func (c *Connectivity) Run(ctx context.Context) error {
	if c == nil || c.registry == nil || c.wifiCalling == nil || c.volte == nil {
		return ErrUnavailable
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan error, 2)
	go func() {
		results <- c.wifiCalling.Run(runCtx, c.registry)
	}()
	go func() {
		results <- c.volte.Run(runCtx, c.registry)
	}()

	first := <-results
	// The two access paths form one lifecycle. If either runner exits, stop the
	// other so shutdown cannot leave one IMS access path behind.
	cancel()
	return errors.Join(first, <-results)
}

func (c *Connectivity) change(modem *mmodem.Modem, change func() error) error {
	modemID := ""
	if modem != nil {
		modemID = strings.TrimSpace(modem.EquipmentIdentifier)
	}
	unlock := c.lockModem(modemID)
	defer unlock()
	return change()
}

func (c *Connectivity) lockModem(modemID string) func() {
	c.operationMu.Lock()
	if c.operations == nil {
		c.operations = make(map[string]*sync.Mutex)
	}
	lock := c.operations[modemID]
	if lock == nil {
		lock = new(sync.Mutex)
		c.operations[modemID] = lock
	}
	c.operationMu.Unlock()

	lock.Lock()
	return lock.Unlock
}
