package call

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// MediaHandler serves ICE configuration and WebRTC signaling sessions.
type MediaHandler struct {
	registry *mmodem.Registry
	media    *Media
}

// NewMediaHandler creates media endpoints independently of history and call actions.
func NewMediaHandler(registry *mmodem.Registry, media *Media) *MediaHandler {
	return &MediaHandler{registry: registry, media: media}
}

func (h *MediaHandler) WebRTCICEServers(c *echo.Context) error {
	servers, err := h.media.WebRTCICEServers(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "list_webrtc_ice_servers_failed", err)
	}
	return c.JSON(http.StatusOK, buildWebRTCICEServersResponse(servers))
}

func (h *MediaHandler) WebRTCSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "call_media_unavailable")
	}
	conn, err := callWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	return h.serveWebRTCSession(c.Request().Context(), conn, modem, callIDParam(c))
}

func (h *MediaHandler) serveWebRTCSession(ctx context.Context, conn *websocket.Conn, modem *mmodem.Modem, callID string) error {
	ctx, cancel := context.WithCancel(ctx)
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	var workers sync.WaitGroup
	defer func() {
		cancel()
		stopClose()
		workers.Wait()
	}()
	var (
		session *WebRTCSession
		writeMu sync.Mutex
	)
	defer func() {
		if session != nil {
			session.CloseIfNotConnected(ctx)
		}
	}()

	for {
		var message WebRTCSignalMessage
		if err := conn.ReadJSON(&message); err != nil {
			return nil
		}
		switch message.Type {
		case "offer":
			if message.Offer == nil || session != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, "invalid WebRTC offer")
				return nil
			}
			nextSession, err := h.media.OpenClientSession(ctx, modem, ClientMedia{CallID: callID, ClientID: message.ClientID})
			if err != nil {
				code := "call_media_unavailable"
				if errors.Is(err, ErrCallInUse) {
					code = "call_in_use"
				}
				// The connection is closing; a second write cannot recover this error.
				_ = writeWebRTCSignal(conn, &writeMu, WebRTCSignalMessage{Type: "error", Code: code, Message: callMediaMessage(err)})
				return nil
			}
			session = nextSession
			workers.Go(func() { writeWebRTCICECandidates(ctx, conn, &writeMu, session.ICECandidates()) })
			answer, err := session.Answer(ctx, WebRTCSessionDescription{
				Type: message.Offer.Type,
				SDP:  message.Offer.SDP,
			})
			if err != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, callMediaMessage(err))
				return nil
			}
			if err := writeWebRTCSignal(conn, &writeMu, WebRTCSignalMessage{
				Type: "answer",
				Answer: &WebRTCSessionDescription{
					Type: answer.Type,
					SDP:  answer.SDP,
				},
			}); err != nil {
				return nil
			}
		case "candidate":
			if message.Candidate == nil || session == nil {
				continue
			}
			if err := session.AddICECandidate(WebRTCICECandidate{
				Candidate:        message.Candidate.Candidate,
				SDPMid:           message.Candidate.SDPMid,
				SDPMLineIndex:    message.Candidate.SDPMLineIndex,
				UsernameFragment: message.Candidate.UsernameFragment,
			}); err != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, callMediaMessage(err))
				return nil
			}
		default:
			_ = writeWebRTCSignalError(conn, &writeMu, "invalid WebRTC signal")
			return nil
		}
	}
}

func writeWebRTCICECandidates(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, candidates <-chan WebRTCICECandidate) {
	for {
		select {
		case <-ctx.Done():
			return
		case candidate, ok := <-candidates:
			if !ok {
				return
			}
			if err := writeWebRTCSignal(conn, writeMu, WebRTCSignalMessage{
				Type: "candidate",
				Candidate: &WebRTCICECandidate{
					Candidate:        candidate.Candidate,
					SDPMid:           candidate.SDPMid,
					SDPMLineIndex:    candidate.SDPMLineIndex,
					UsernameFragment: candidate.UsernameFragment,
				},
			}); err != nil {
				return
			}
		}
	}
}

func writeWebRTCSignalError(conn *websocket.Conn, writeMu *sync.Mutex, message string) error {
	return writeWebRTCSignal(conn, writeMu, WebRTCSignalMessage{Type: "error", Message: message})
}

func writeWebRTCSignal(conn *websocket.Conn, writeMu *sync.Mutex, message WebRTCSignalMessage) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(message)
}
