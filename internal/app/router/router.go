package router

import (
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/damonto/sigmo/internal/app/auth"
	hauth "github.com/damonto/sigmo/internal/app/handler/auth"
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

func Register(e *echo.Echo, cfg *config.Config, manager *modem.Manager) {
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
	authHandler := hauth.New(cfg, authStore)
	v1.GET("/auth/otp/required", authHandler.OTPRequirement)
	v1.POST("/auth/otp", authHandler.SendOTP)
	v1.POST("/auth/otp/verify", authHandler.VerifyOTP)
	protected := v1.Group("")
	if cfg.App.OTPRequired {
		protected.Use(appmiddleware.Auth(authStore))
	}
	internetConnector := pinternet.NewConnector()

	{
		h := hmodem.New(cfg, manager, internetConnector)
		protected.GET("/modems", h.List)
		protected.GET("/modems/:id", h.Get)
		protected.PUT("/modems/:id/sim-slots/:identifier", h.SwitchSimSlot)
		protected.PUT("/modems/:id/msisdn", h.UpdateMSISDN)
		protected.GET("/modems/:id/settings", h.GetSettings)
		protected.PUT("/modems/:id/settings", h.UpdateSettings)

		{
			h := message.New(manager)
			protected.GET("/modems/:id/messages", h.List)
			protected.GET("/modems/:id/messages/:participant", h.ListByParticipant)
			protected.POST("/modems/:id/messages", h.Send)
			protected.DELETE("/modems/:id/messages/:participant", h.DeleteByParticipant)
		}

		{
			h := ussd.New(manager)
			protected.POST("/modems/:id/ussd", h.Execute)
		}

		{
			h := network.New(manager)
			protected.GET("/modems/:id/networks", h.List)
			protected.PUT("/modems/:id/networks/:operatorCode", h.Register)
		}

		{
			h := hinternet.NewWithConnector(manager, internetConnector)
			protected.GET("/modems/:id/internet-connections/current", h.Current)
			protected.GET("/modems/:id/internet-connections/public", h.Public)
			protected.POST("/modems/:id/internet-connections", h.Connect)
			protected.DELETE("/modems/:id/internet-connections/current", h.Disconnect)
		}

		{
			h := euicc.New(cfg, manager)
			protected.GET("/modems/:id/euicc", h.Get)
		}

		{
			h := esim.New(cfg, manager, internetConnector)
			protected.GET("/modems/:id/esims", h.List)
			protected.GET("/modems/:id/esims/discover", h.Discover)
			protected.GET("/modems/:id/esims/download", h.Download)
			protected.POST("/modems/:id/esims/:iccid/enabling", h.Enable)
			protected.PUT("/modems/:id/esims/:iccid/nickname", h.UpdateNickname)
			protected.DELETE("/modems/:id/esims/:iccid", h.Delete)
		}

		{
			h := notification.New(cfg, manager)
			protected.GET("/modems/:id/notifications", h.List)
			protected.POST("/modems/:id/notifications/:sequence/resend", h.Resend)
			protected.DELETE("/modems/:id/notifications/:sequence", h.Delete)
		}
	}
}
