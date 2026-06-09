package router

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/forwarder"
	hauth "github.com/damonto/sigmo/internal/app/handler/auth"
	hcall "github.com/damonto/sigmo/internal/app/handler/call"
	"github.com/damonto/sigmo/internal/app/handler/capability"
	"github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/handler/euicc"
	hinternet "github.com/damonto/sigmo/internal/app/handler/internet"
	"github.com/damonto/sigmo/internal/app/handler/message"
	hmodem "github.com/damonto/sigmo/internal/app/handler/modem"
	"github.com/damonto/sigmo/internal/app/handler/network"
	"github.com/damonto/sigmo/internal/app/handler/notification"
	hsettings "github.com/damonto/sigmo/internal/app/handler/settings"
	"github.com/damonto/sigmo/internal/app/handler/ussd"
	hwebsheet "github.com/damonto/sigmo/internal/app/handler/websheet"
	appmiddleware "github.com/damonto/sigmo/internal/app/middleware"
	pcall "github.com/damonto/sigmo/internal/pkg/call"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	pwebsheet "github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
	"github.com/damonto/sigmo/web"
)

type RegisterConfig struct {
	Store              *settings.Store
	Registry           *modem.Registry
	Internet           *pinternet.Connector
	Relay              *forwarder.Relay
	NetworkPreferences *modem.NetworkPreferences
	Storage            *storage.Store
	WiFiCalling        wificalling.Coordinator
	Calls              *pcall.Service
	Websheets          *pwebsheet.Broker
}

func Register(e *echo.Echo, deps RegisterConfig) error {
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Filesystem: web.Root(),
		Index:      "index.html",
		HTML5:      true,
		Skipper: func(c *echo.Context) bool {
			path := c.Request().URL.Path
			return strings.HasPrefix(path, "/api/")
		},
	}))

	v1 := e.Group("/api/v1")

	capabilityHandler := capability.New()
	v1.GET("/capabilities", capabilityHandler.List)

	authStore := auth.NewStore()
	authHandler := hauth.New(deps.Store, authStore)
	v1.GET("/auth/otp/required", authHandler.OTPRequirement)
	v1.POST("/auth/otp", authHandler.SendOTP)
	v1.POST("/auth/otp/verify", authHandler.VerifyOTP)
	protected := v1.Group("")
	protected.Use(appmiddleware.Auth(authStore, deps.Store))
	{
		{
			h := hsettings.New(deps.Store, deps.Internet, deps.Relay)
			protected.GET("/settings", h.Get)
			protected.PUT("/settings", h.Update)
		}

		hwebsheet.New(deps.Websheets).Register(protected)

		h := hmodem.New(deps.Store, deps.Registry, deps.Internet, deps.WiFiCalling)
		protected.GET("/modems", h.List)
		protected.GET("/modems/:id", h.Get)
		protected.POST("/modems/:id/sim-unlocks", h.UnlockSIM)
		protected.PUT("/modems/:id/sim-slots/:identifier", h.SwitchSimSlot)
		protected.PUT("/modems/:id/msisdn", h.UpdateMSISDN)
		protected.GET("/modems/:id/settings", h.Settings)
		protected.PUT("/modems/:id/settings", h.UpdateSettings)
		protected.GET("/modems/:id/wifi-calling-settings", h.WiFiCallingSettings)
		protected.PUT("/modems/:id/wifi-calling-settings", h.UpdateWiFiCallingSettings)
		protected.POST("/modems/:id/wifi-calling-sessions", h.CreateWiFiCallingSession)
		protected.POST("/modems/:id/wifi-calling-websheets", h.StartWiFiCallingWebsheet)
		protected.POST("/modems/:id/wifi-calling-emergency-address-websheets", h.StartWiFiCallingEmergencyAddressWebsheet)

		{
			h := message.New(deps.Registry, deps.Storage, deps.WiFiCalling)
			protected.GET("/modems/:id/messages", h.List)
			protected.GET("/modems/:id/messages/:participant", h.ListByParticipant)
			protected.POST("/modems/:id/messages", h.Send)
			protected.DELETE("/modems/:id/messages/:participant", h.DeleteByParticipant)
		}

		{
			h := hcall.New(deps.Registry, deps.Calls)
			protected.GET("/call-media/ice-servers", h.WebRTCICEServers)
			protected.GET("/modems/:id/calls", h.List)
			protected.POST("/modems/:id/calls", h.Dial)
			protected.GET("/modems/:id/calls/events", h.Events)
			protected.GET("/modems/:id/calls/:callID/webrtc-sessions", h.WebRTCSession)
			protected.POST("/modems/:id/calls/:callID/dtmf-events", h.SendDTMF)
			protected.PATCH("/modems/:id/calls/:callID", h.Update)
			protected.DELETE("/modems/:id/calls/:callID", h.Delete)
		}

		{
			h := ussd.New(deps.Registry, deps.WiFiCalling)
			protected.POST("/modems/:id/ussd", h.Execute)
		}

		{
			h, err := network.New(deps.Registry, deps.NetworkPreferences, deps.Storage)
			if err != nil {
				return fmt.Errorf("configure network handler: %w", err)
			}
			protected.GET("/modems/:id/networks", h.List)
			protected.GET("/modems/:id/networks/modes", h.Modes)
			protected.PUT("/modems/:id/networks/current-modes", h.SetCurrentModes)
			protected.GET("/modems/:id/networks/bands", h.Bands)
			protected.PUT("/modems/:id/networks/current-bands", h.SetCurrentBands)
			protected.PUT("/modems/:id/networks/:operatorCode", h.Register)
		}

		{
			h := hinternet.New(deps.Registry, deps.Internet)
			protected.GET("/modems/:id/internet-connections/current", h.Current)
			protected.GET("/modems/:id/internet-connections/public", h.Public)
			protected.POST("/modems/:id/internet-connections", h.Connect)
			protected.DELETE("/modems/:id/internet-connections/current", h.Disconnect)
		}

		{
			h := euicc.New(deps.Store, deps.Registry)
			protected.GET("/modems/:id/euicc", h.Get)
		}

		{
			h := esim.New(deps.Store, deps.Registry, deps.Internet, deps.Websheets)
			protected.GET("/modems/:id/esims", h.List)
			protected.POST("/modems/:id/esim-discoveries", h.Discovery)
			protected.GET("/modems/:id/esims/download-sessions", h.Download)
			h.RegisterTransferRoutes(protected)
			protected.PUT("/modems/:id/esims/:iccid/activation", h.Enable)
			protected.PUT("/modems/:id/esims/:iccid/nickname", h.UpdateNickname)
			protected.DELETE("/modems/:id/esims/:iccid", h.Delete)
		}

		{
			h := notification.New(deps.Store, deps.Registry)
			protected.GET("/modems/:id/notifications", h.List)
			protected.POST("/modems/:id/notifications/:sequence/deliveries", h.Resend)
			protected.DELETE("/modems/:id/notifications/:sequence", h.Delete)
		}
	}
	return nil
}
