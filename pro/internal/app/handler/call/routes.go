//go:build wifi_calling

package call

import (
	"github.com/labstack/echo/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	procall "github.com/damonto/sigmo/pro/internal/pkg/call"
)

func Register(group *echo.Group, registry *mmodem.Registry, calls *procall.Service, media *procall.Media) {
	h := New(registry, calls, media)
	group.GET("/call-media/ice-servers", h.WebRTCICEServers)
	group.GET("/modems/:id/calls", h.List)
	group.POST("/modems/:id/calls", h.Dial)
	group.GET("/modems/:id/calls/events", h.Events)
	group.GET("/modems/:id/calls/:callID/webrtc-sessions", h.WebRTCSession)
	group.POST("/modems/:id/calls/:callID/dtmf-events", h.SendDTMF)
	group.PATCH("/modems/:id/calls/:callID", h.Update)
	group.DELETE("/modems/:id/calls/:callID", h.Delete)
}
