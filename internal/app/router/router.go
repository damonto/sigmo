package router

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/handler/appinfo"
	hauth "github.com/damonto/sigmo/internal/app/handler/auth"
	"github.com/damonto/sigmo/internal/app/handler/capability"
	"github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/handler/euicc"
	hinternet "github.com/damonto/sigmo/internal/app/handler/internet"
	"github.com/damonto/sigmo/internal/app/handler/message"
	hmodem "github.com/damonto/sigmo/internal/app/handler/modem"
	"github.com/damonto/sigmo/internal/app/handler/network"
	"github.com/damonto/sigmo/internal/app/handler/notification"
	hsettings "github.com/damonto/sigmo/internal/app/handler/settings"
	"github.com/damonto/sigmo/internal/app/handler/simapp"
	"github.com/damonto/sigmo/internal/app/handler/ussd"
	hwebpush "github.com/damonto/sigmo/internal/app/handler/webpush"
	appmiddleware "github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/app/modemstatus"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	pmessage "github.com/damonto/sigmo/internal/pkg/message"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	pussd "github.com/damonto/sigmo/internal/pkg/ussd"
	"github.com/damonto/sigmo/internal/pkg/webpush"
	"github.com/damonto/sigmo/web"
)

type Extension func(*echo.Group, RegisterConfig) error

type RegisterConfig struct {
	BuildVersion       string
	Store              *settings.Store
	Registry           *modem.Registry
	Internet           *pinternet.Connector
	Relay              *forwarder.Relay
	NetworkPreferences *modem.NetworkPreferences
	Storage            *storage.Store
	WebPush            *webpush.Client
	Reminders          *reminder.Scheduler
	MessageRoute       pmessage.Route
	USSDRoute          pussd.Route
	ModemOverview      []modemstatus.Extension
	Features           []string
	Extensions         []Extension
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

	appInfoHandler := appinfo.New(deps.BuildVersion)
	v1.GET("/app", appInfoHandler.Get)

	capabilityHandler := capability.New(deps.Features)
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
			protected.PUT("/settings/auth", h.UpdateAuth)
			protected.PUT("/settings/proxy", h.UpdateProxy)
			protected.PUT("/settings/notifications/:channel", h.UpdateNotificationChannel)
		}

		h := hmodem.New(deps.Store, deps.Registry, deps.Internet, deps.Reminders, deps.ModemOverview...)
		protected.GET("/modems", h.List)
		protected.GET("/modems/:id", h.Get)
		protected.POST("/modems/:id/sim-unlocks", h.UnlockSIM)
		protected.PUT("/modems/:id/sim-slots/:identifier", h.SwitchSimSlot)
		protected.PUT("/modems/:id/msisdn", h.UpdateMSISDN)
		protected.PUT("/modems/:id/sims/:iccid/reminder", h.UpdateReminder)
		protected.DELETE("/modems/:id/sims/:iccid/reminder", h.DeleteReminder)
		protected.GET("/modems/:id/settings", h.Settings)
		protected.PUT("/modems/:id/settings", h.UpdateSettings)

		{
			h := simapp.New(deps.Registry)
			protected.GET("/modems/:id/sim-application/sessions", h.Session)
		}

		{
			h := message.New(deps.Registry, deps.Storage, deps.MessageRoute)
			protected.GET("/modems/:id/messages", h.List)
			protected.GET("/modems/:id/messages/:participant", h.ListByParticipant)
			protected.POST("/modems/:id/messages", h.Send)
			protected.DELETE("/modems/:id/messages/:participant", h.DeleteByParticipant)
		}

		{
			h := ussd.New(deps.Registry, deps.USSDRoute)
			protected.POST("/modems/:id/ussd", h.Execute)
		}

		{
			h, err := network.New(deps.Registry, deps.NetworkPreferences, deps.Storage)
			if err != nil {
				return fmt.Errorf("configure network handler: %w", err)
			}
			protected.GET("/modems/:id/networks", h.List)
			protected.GET("/modems/:id/networks/airplane-mode", h.AirplaneMode)
			protected.PUT("/modems/:id/networks/airplane-mode", h.SetAirplaneMode)
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
			protected.PUT("/modems/:id/internet-connections/current/preferences", h.UpdatePreferences)
			protected.DELETE("/modems/:id/internet-connections/current", h.Disconnect)
		}

		{
			h := euicc.New(deps.Store, deps.Registry)
			protected.GET("/modems/:id/ses", h.Get)
		}

		{
			h := esim.New(esim.Config{
				Store:     deps.Store,
				Registry:  deps.Registry,
				Internet:  deps.Internet,
				Reminders: deps.Reminders,
			})
			protected.GET("/modems/:id/esims", h.List)
			protected.POST("/modems/:id/ses/:seId/esim-discoveries", h.Discovery)
			protected.GET("/modems/:id/esim-downloads/sessions", h.Download)
			protected.PUT("/modems/:id/ses/:seId/esims/:iccid/activation", h.Enable)
			protected.PUT("/modems/:id/ses/:seId/esims/:iccid/nickname", h.UpdateNickname)
			protected.DELETE("/modems/:id/ses/:seId/esims/:iccid", h.Delete)
			protected.PUT("/modems/:id/ses/:seId/esims/:iccid/reminder", h.UpdateReminder)
			protected.DELETE("/modems/:id/ses/:seId/esims/:iccid/reminder", h.DeleteReminder)
		}

		{
			h := notification.New(deps.Store, deps.Registry)
			protected.GET("/modems/:id/notifications", h.List)
			protected.POST("/modems/:id/ses/:seId/notifications/:sequence/deliveries", h.Resend)
			protected.DELETE("/modems/:id/ses/:seId/notifications/:sequence", h.Delete)
		}

		{
			h := hwebpush.New(deps.WebPush)
			protected.GET("/web-push", h.Get)
			protected.PUT("/web-push", h.Update)
			protected.GET("/web-push/subscriptions", h.ListSubscriptions)
			protected.POST("/web-push/subscriptions", h.Register)
			protected.PATCH("/web-push/subscriptions/:subscriptionId", h.Rename)
			protected.DELETE("/web-push/subscriptions/:subscriptionId", h.Delete)
		}

		for _, extension := range deps.Extensions {
			if err := extension(protected, deps); err != nil {
				return err
			}
		}
	}
	return nil
}
