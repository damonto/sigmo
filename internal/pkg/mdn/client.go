package mdn

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/damonto/ims-go/eapaka"
	usim "github.com/damonto/wwan-go/sim"
)

const (
	prefetchNonce         = "prefetch90123456789012345"
	maxResponseSize       = 1 << 20
	lookupTimeout         = 100 * time.Second
	statusPNRNotAvailable = 6020
)

var globalNumber = regexp.MustCompile(`^\+[1-9][0-9]{0,14}$`)

// Authenticate performs AKA on the current SIM without transferring key ownership.
type Authenticate func(context.Context, []byte, []byte) (usim.AKAResult, error)

// StatusError identifies a carrier or HTTP failure without exposing response data.
type StatusError struct {
	Action     string
	Code       int
	HTTP       bool
	RetryAfter time.Duration

	// NumberPresent records only whether an unsuccessful response carried a
	// nonempty number. Never include the number itself in an error or log.
	NumberPresent bool
}

func (e *StatusError) Error() string {
	if e.HTTP {
		return fmt.Sprintf("MDN HTTP status %d", e.Code)
	}
	if e.Code == statusPNRNotAvailable {
		return fmt.Sprintf("MDN %s status %d (PNR not available; number_present=%t)", e.Action, e.Code, e.NumberPresent)
	}
	return fmt.Sprintf("MDN %s status %d", e.Action, e.Code)
}

// Temporary reports whether an error permits a later lookup attempt.
func Temporary(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if status, ok := errors.AsType[*StatusError](err); ok {
		if status.HTTP {
			return status.Code == http.StatusTooManyRequests || (status.Code >= 500 && status.Code < 600)
		}
		return status.Code == 6005 || status.Code == 6050
	}
	var network net.Error
	return errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF)
}

// Client implements independent MDN prefetch over carrier HTTPS services.
type Client struct{ http *http.Client }

// NewClient creates a client with bounded requests and no redirects.
func NewClient(client *http.Client) *Client {
	var configured http.Client
	if client != nil {
		configured = *client
	}
	if configured.Timeout <= 0 || configured.Timeout > 25*time.Second {
		configured.Timeout = 25 * time.Second
	}
	configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &configured}
}

type request struct {
	Action     string `json:"action-name"`
	ID         int    `json:"request-id"`
	AuthType   string `json:"auth-type,omitempty"`
	Subscriber string `json:"subscriber-id,omitempty"`
	UniqueID   string `json:"unique-id,omitempty"`
	AccountID  string `json:"device-account-identifier,omitempty"`
	Payload    string `json:"payload,omitempty"`
	Nonce      string `json:"client-nonce,omitempty"`
}

type response struct {
	ID        *int   `json:"response-id"`
	Status    *int   `json:"status"`
	Challenge string `json:"challenge"`
	Number    string `json:"phone-number"`
}

// Lookup returns a carrier-reported international phone number after authentication.
// It does not register IMS, send SMS, or verify Apple's phone-number signature.
func (c *Client) Lookup(ctx context.Context, profile Profile, identity Identity, authenticate Authenticate) (string, error) {
	if profile.Disabled {
		return "", ErrUnsupported
	}
	if err := profile.validate(); err != nil {
		return "", err
	}
	if !matches(profile, identity) {
		return "", ErrUnsupported
	}
	if authenticate == nil || !digits.MatchString(identity.IMSI) || len(identity.IMSI) > 15 || len(identity.IMSI) < 5 ||
		len(identity.MCC) != 3 || !digits.MatchString(identity.MCC) ||
		(len(identity.MNC) != 2 && len(identity.MNC) != 3) || !digits.MatchString(identity.MNC) ||
		len(identity.IMEI) != 15 || !digits.MatchString(identity.IMEI) {
		return "", errors.New("MDN requires the current SIM and modem identity")
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	mnc := identity.MNC
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	template := profile.IdentityTemplate
	if template == "" {
		template = "0$IMSI@nai.epc.mnc$MNC.mcc$MCC.3gppnetwork.org"
	}
	nai := strings.NewReplacer("$IMSI", identity.IMSI, "$MNC", mnc, "$MCC", identity.MCC).Replace(template)
	packet, err := (eapaka.EAPPacket{Code: 2, Type: 1, HasType: true, Data: []byte(nai)}).MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("encode MDN identity: %w", err)
	}
	account := make([]byte, 16)
	if _, err := rand.Read(account); err != nil {
		return "", fmt.Errorf("generate MDN account identifier: %w", err)
	}
	account[6] = account[6]&15 | 64
	account[8] = account[8]&63 | 128
	accountID := fmt.Sprintf("%X-%X-%X-%X-%X", account[:4], account[4:6], account[6:8], account[8:10], account[10:])
	phone := request{Action: "getPhoneNumber", ID: 3, Nonce: base64.StdEncoding.EncodeToString([]byte(prefetchNonce))}
	auth := request{Action: "getAuthentication", ID: 1, AuthType: "EAP-AKA", Subscriber: base64.StdEncoding.EncodeToString(packet), UniqueID: identity.IMEI, AccountID: accountID}
	synchronized := false
	for round := range 3 {
		responses, err := c.invoke(ctx, profile, []request{auth, phone})
		if err != nil {
			return "", err
		}
		authReply, err := findResponse(responses, auth.ID)
		if err != nil {
			return "", err
		}
		switch *authReply.Status {
		case 6000:
			reply, err := findResponse(responses, phone.ID)
			if err != nil {
				return "", err
			}
			if *reply.Status != 6000 {
				return "", &StatusError{Action: phone.Action, Code: *reply.Status, NumberPresent: reply.Number != ""}
			}
			if !globalNumber.MatchString(reply.Number) {
				return "", errors.New("MDN response has an invalid phone number")
			}
			return reply.Number, nil
		case 6302:
			if round == 2 {
				return "", errors.New("MDN authentication exceeded challenge limit")
			}
			payload, syncFailure, err := respond(ctx, nai, authReply.Challenge, authenticate)
			if err != nil {
				return "", err
			}
			if syncFailure && synchronized {
				return "", errors.New("MDN AKA synchronization failed repeatedly")
			}
			synchronized = synchronized || syncFailure
			auth = request{Action: "postChallenge", ID: 2 + round*2, Payload: payload}
		default:
			return "", &StatusError{Action: auth.Action, Code: *authReply.Status}
		}
	}
	return "", errors.New("MDN authentication did not complete")
}

func findResponse(responses []response, id int) (response, error) {
	var found response
	seen := make(map[int]bool, len(responses))
	for _, reply := range responses {
		if reply.ID == nil || reply.Status == nil {
			return response{}, errors.New("MDN response is missing an ID or status")
		}
		if seen[*reply.ID] {
			return response{}, errors.New("MDN response has duplicate IDs")
		}
		seen[*reply.ID] = true
		if *reply.ID == id {
			found = reply
		}
	}
	if found.ID == nil {
		return response{}, errors.New("MDN response ID does not match the request")
	}
	return found, nil
}

func respond(ctx context.Context, identity, text string, authenticate Authenticate) (string, bool, error) {
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", false, errors.New("invalid MDN challenge encoding")
	}
	var message eapaka.AKAMessage
	if err := message.UnmarshalBinary(data); err != nil {
		return "", false, fmt.Errorf("decode MDN challenge: %w", err)
	}
	if message.EAPType != 23 {
		return "", false, errors.New("unsupported MDN EAP type")
	}
	challenge, err := eapaka.AKAParametersFromChallenge(message)
	if err != nil {
		return "", false, fmt.Errorf("decode MDN AKA challenge: %w", err)
	}
	aka, err := authenticate(ctx, challenge.RAND, challenge.AUTN)
	if err != nil {
		return "", false, fmt.Errorf("authenticate MDN SIM: %w", err)
	}
	if aka.Reject {
		return "", false, errors.New("MDN SIM authentication rejected")
	}
	syncFailure := len(aka.AUTS) != 0
	if syncFailure {
		data, err = eapaka.BuildAKASynchronizationFailure(challenge.Identifier, aka.AUTS)
	} else {
		keys, keyErr := eapaka.DeriveAKAKeys([]byte(identity), aka.IK, aka.CK)
		if keyErr != nil {
			return "", false, keyErr
		}
		if eapaka.HasAttribute(message.Attributes, 11) {
			if err := message.VerifyMAC(keys.AUT, nil); err != nil {
				return "", false, fmt.Errorf("verify MDN challenge MAC: %w", err)
			}
		}
		data, err = eapaka.BuildAKAResponse(challenge.Identifier, aka.RES, keys.AUT)
	}
	if err != nil {
		return "", false, err
	}
	return base64.StdEncoding.EncodeToString(data), syncFailure, nil
}

func (c *Client) invoke(ctx context.Context, profile Profile, requests []request) ([]response, error) {
	payload, err := json.Marshal(requests)
	if err != nil {
		return nil, fmt.Errorf("encode MDN request: %w", err)
	}
	var body bytes.Buffer
	writer := gzip.NewWriter(&body)
	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, profile.Server, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("x-protocol-version", profile.ProtocolVersion)
	req.Header.Set("x-country-iso-code", strings.ToLower(profile.CountryISO))
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("User-Agent", profile.UserAgent)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query MDN entitlement: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		failure := &StatusError{Action: "request", Code: res.StatusCode, HTTP: true}
		if seconds, err := strconv.ParseInt(res.Header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
			failure.RetryAfter = time.Duration(min(seconds, 86400)) * time.Second
		} else if at, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
			failure.RetryAfter = max(0, min(time.Until(at), 24*time.Hour))
		}
		return nil, failure
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseSize {
		return nil, errors.New("MDN response exceeds size limit")
	}
	var responses []response
	if err := json.Unmarshal(data, &responses); err != nil {
		return nil, errors.New("invalid MDN response JSON")
	}
	return responses, nil
}
