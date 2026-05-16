package router

import (
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/forwarder"
	hauth "github.com/damonto/sigmo/internal/app/handler/auth"
	hconfig "github.com/damonto/sigmo/internal/app/handler/config"
	"github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/handler/euicc"
	hinternet "github.com/damonto/sigmo/internal/app/handler/internet"
	"github.com/damonto/sigmo/internal/app/handler/message"
	hmodem "github.com/damonto/sigmo/internal/app/handler/modem"
	"github.com/damonto/sigmo/internal/app/handler/network"
	"github.com/damonto/sigmo/internal/app/handler/notification"
	"github.com/damonto/sigmo/internal/app/handler/ussd"
	appmiddleware "github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/pkg/config"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/web"
)

type RegisterConfig struct {
	Store              *config.Store
	Modems             *modem.Manager
	Internet           *pinternet.Connector
	Relay              *forwarder.Relay
	NetworkPreferences *modem.NetworkPreferences
}

func Register(e *echo.Echo, cfg RegisterConfig) {
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

	authStore := auth.NewStore()
	authHandler := hauth.New(cfg.Store, authStore)
	v1.GET("/auth/otp/required", authHandler.OTPRequirement)
	v1.POST("/auth/otp", authHandler.SendOTP)
	v1.POST("/auth/otp/verify", authHandler.VerifyOTP)
	protected := v1.Group("")
	protected.Use(appmiddleware.Auth(authStore, cfg.Store))
	{
		{
			h := hconfig.New(cfg.Store, cfg.Internet, cfg.Relay)
			protected.GET("/config", h.Get)
			protected.PUT("/config", h.Update)
		}

		h := hmodem.New(cfg.Store, cfg.Modems, cfg.Internet)
		protected.GET("/modems", h.List)
		protected.GET("/modems/:id", h.Get)
		protected.PUT("/modems/:id/sim-slots/:identifier", h.SwitchSimSlot)
		protected.PUT("/modems/:id/msisdn", h.UpdateMSISDN)
		protected.GET("/modems/:id/settings", h.GetSettings)
		protected.PUT("/modems/:id/settings", h.UpdateSettings)

		{
			h := message.New(cfg.Modems)
			protected.GET("/modems/:id/messages", h.List)
			protected.GET("/modems/:id/messages/:participant", h.ListByParticipant)
			protected.POST("/modems/:id/messages", h.Send)
			protected.DELETE("/modems/:id/messages/:participant", h.DeleteByParticipant)
		}

		{
			h := ussd.New(cfg.Modems)
			protected.POST("/modems/:id/ussd", h.Execute)
		}

		{
			h := network.New(cfg.Modems, cfg.NetworkPreferences)
			protected.GET("/modems/:id/networks", h.List)
			protected.GET("/modems/:id/networks/modes", h.Modes)
			protected.PUT("/modems/:id/networks/current-modes", h.SetCurrentModes)
			protected.GET("/modems/:id/networks/bands", h.Bands)
			protected.PUT("/modems/:id/networks/current-bands", h.SetCurrentBands)
			protected.PUT("/modems/:id/networks/:operatorCode", h.Register)
		}

		{
			h := hinternet.New(cfg.Modems, cfg.Internet)
			protected.GET("/modems/:id/internet-connections/current", h.Current)
			protected.GET("/modems/:id/internet-connections/public", h.Public)
			protected.POST("/modems/:id/internet-connections", h.Connect)
			protected.DELETE("/modems/:id/internet-connections/current", h.Disconnect)
		}

		{
			h := euicc.New(cfg.Store, cfg.Modems)
			protected.GET("/modems/:id/euicc", h.Get)
		}

		{
			h := esim.New(cfg.Store, cfg.Modems, cfg.Internet)
			protected.GET("/modems/:id/esims", h.List)
			protected.GET("/modems/:id/esims/discover", h.Discover)
			protected.GET("/modems/:id/esims/download-sessions", h.Download)
			protected.PUT("/modems/:id/esims/:iccid/activation", h.Enable)
			protected.PUT("/modems/:id/esims/:iccid/nickname", h.UpdateNickname)
			protected.DELETE("/modems/:id/esims/:iccid", h.Delete)
		}

		{
			h := notification.New(cfg.Store, cfg.Modems)
			protected.GET("/modems/:id/notifications", h.List)
			protected.POST("/modems/:id/notifications/:sequence/deliveries", h.Resend)
			protected.DELETE("/modems/:id/notifications/:sequence", h.Delete)
		}
	}
}
