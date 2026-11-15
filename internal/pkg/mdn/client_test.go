package mdn

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/damonto/ims-go/eapaka"
	usim "github.com/damonto/wwan-go/sim"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testProfile() Profile {
	return Profile{MCC: "001", MNC: "01", Server: "https://carrier.example/mdn", ProtocolVersion: "2", SigningVersion: 2, UserAgent: "Entitlement/2 (iPhone) iOS/27.3", CountryISO: "GB"}
}
func testIdentity() Identity {
	return Identity{MCC: "001", MNC: "01", IMSI: "001010123456789", IMEI: "123456789012345"}
}
func httpReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func readRequests(t *testing.T, r *http.Request) []request {
	t.Helper()
	if r.Method != "POST" || r.Header.Get("Content-Encoding") != "gzip" || r.Header.Get("x-protocol-version") != "2" || r.Header.Get("x-country-iso-code") != "gb" {
		t.Fatal("incorrect entitlement request headers")
	}
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var requests []request
	if err := json.NewDecoder(gz).Decode(&requests); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[1].Action != "getPhoneNumber" || requests[1].ID != 3 || requests[1].Nonce != base64.StdEncoding.EncodeToString([]byte("prefetch90123456789012345")) {
		t.Fatal("missing batched number request or incorrect prefetch nonce")
	}
	return requests
}
func testChallenge(t *testing.T, badMAC bool) (string, usim.AKAResult) {
	t.Helper()
	aka := usim.AKAResult{IK: bytes.Repeat([]byte{1}, 16), CK: bytes.Repeat([]byte{2}, 16), RES: bytes.Repeat([]byte{3}, 8)}
	message := eapaka.AKAMessage{Code: 1, Identifier: 7, Subtype: 1, Attributes: []eapaka.AKAAttribute{
		{Type: 1, Value: append([]byte{0, 0}, bytes.Repeat([]byte{4}, 16)...)},
		{Type: 2, Value: append([]byte{0, 0}, bytes.Repeat([]byte{5}, 16)...)},
	}}
	if badMAC {
		message.Attributes = append(message.Attributes, eapaka.AKAAttribute{Type: 11, Value: make([]byte, 18)})
	}
	data, err := message.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(data), aka
}
func TestLookupBatchedAKA(t *testing.T) {
	for _, resync := range []bool{false, true} {
		t.Run(fmt.Sprint("resync=", resync), func(t *testing.T) {
			challenge, aka := testChallenge(t, false)
			calls, simCalls := 0, 0
			var account string
			client := NewClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				requests := readRequests(t, r)
				auth := requests[0]
				calls++
				if calls == 1 {
					packet, err := base64.StdEncoding.DecodeString(auth.Subscriber)
					if err != nil {
						t.Fatal(err)
					}
					if len(packet) < 5 || packet[0] != 2 || packet[4] != 1 || int(binary.BigEndian.Uint16(packet[2:4])) != len(packet) || string(packet[5:]) != "0001010123456789@nai.epc.mnc001.mcc001.3gppnetwork.org" || auth.UniqueID != testIdentity().IMEI || auth.AuthType != "EAP-AKA" || len(auth.AccountID) != 36 {
						t.Fatal("incorrect initial SIM identity or device account")
					}
					account = auth.AccountID
				} else {
					if auth.Action != "postChallenge" || auth.Subscriber != "" || auth.AccountID != "" || auth.ID != 2+(calls-2)*2 {
						t.Fatal("incorrect challenge continuation")
					}
					data, err := base64.StdEncoding.DecodeString(auth.Payload)
					if err != nil {
						t.Fatal(err)
					}
					var msg eapaka.AKAMessage
					if err := msg.UnmarshalBinary(data); err != nil {
						t.Fatal(err)
					}
					if msg.Identifier != 7 || msg.Code != 2 {
						t.Fatal("incorrect EAP challenge response")
					}
					if resync && calls == 2 {
						if msg.Subtype != 4 {
							t.Fatal("missing synchronization failure")
						}
					} else {
						keys, err := eapaka.DeriveAKAKeys([]byte("0001010123456789@nai.epc.mnc001.mcc001.3gppnetwork.org"), aka.IK, aka.CK)
						if err != nil {
							t.Fatal(err)
						}
						if err := msg.VerifyMAC(keys.AUT, nil); err != nil {
							t.Fatal(err)
						}
					}
				}
				if calls == 1 || (resync && calls == 2) {
					return httpReply(200, fmt.Sprintf(`[{"response-id":%d,"status":6302,"challenge":%q},{"response-id":3,"status":6001}]`, auth.ID, challenge)), nil
				}
				return httpReply(200, fmt.Sprintf(`[{"response-id":%d,"status":6000},{"response-id":3,"status":6000,"phone-number":"+15551234567","signature":"ignored"}]`, auth.ID)), nil
			})})
			number, err := client.Lookup(t.Context(), testProfile(), testIdentity(), func(ctx context.Context, rand, autn []byte) (usim.AKAResult, error) {
				simCalls++
				if len(rand) != 16 || rand[0] != 4 || len(autn) != 16 || autn[0] != 5 {
					t.Fatal("incorrect AKA vectors")
				}
				if resync && simCalls == 1 {
					return usim.AKAResult{AUTS: make([]byte, 14)}, nil
				}
				return aka, nil
			})
			if err != nil || number != "+15551234567" || account == "" || simCalls != calls-1 {
				t.Fatalf("lookup = %q, %v; HTTP=%d SIM=%d", number, err, calls, simCalls)
			}
		})
	}
}
func TestLookupRejectsInvalidReplies(t *testing.T) {
	for name, body := range map[string]string{
		"missing status":         `[{"response-id":1,"response-status":6000},{"response-id":3,"status":6000,"phone-number":"+15551234567"}]`,
		"duplicate ID":           `[{"response-id":1,"status":6000},{"response-id":1,"status":6000},{"response-id":3,"status":6000,"phone-number":"+15551234567"}]`,
		"duplicate unrelated ID": `[{"response-id":1,"status":6000},{"response-id":2,"status":6000},{"response-id":2,"status":6000},{"response-id":3,"status":6000,"phone-number":"+15551234567"}]`,
		"auth rejected":          `[{"response-id":1,"status":6001},{"response-id":3,"status":6000,"phone-number":"+15551234567"}]`,
		"local number":           `[{"response-id":1,"status":6000},{"response-id":3,"status":6000,"phone-number":"05551234567"}]`,
		"bad challenge":          `[{"response-id":1,"status":6302,"challenge":"private_challenge"}]`,
		"oversized":              strings.Repeat("x", maxResponseSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return httpReply(200, body), nil })})
			number, err := client.Lookup(t.Context(), testProfile(), testIdentity(), func(context.Context, []byte, []byte) (usim.AKAResult, error) {
				t.Fatal("unexpected SIM access")
				return usim.AKAResult{}, nil
			})
			if err == nil || number != "" || strings.Contains(err.Error(), "private_challenge") {
				t.Fatalf("lookup = %q, %v", number, err)
			}
		})
	}
}

func TestLookupAppliesRequestDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1, time.Minute, time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			limit := 25 * time.Second
			if timeout > 0 {
				limit = min(limit, timeout)
			}
			client := NewClient(&http.Client{Timeout: timeout, Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > limit+time.Second {
					t.Error("request did not receive a bounded deadline")
				}
				return httpReply(200, `[{"response-id":1,"status":6000},{"response-id":3,"status":6000,"phone-number":"+15551234567"}]`), nil
			})})
			if _, err := client.Lookup(t.Context(), testProfile(), testIdentity(), func(context.Context, []byte, []byte) (usim.AKAResult, error) {
				t.Fatal("unexpected SIM access")
				return usim.AKAResult{}, nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCancellationIsNotTemporary(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://carrier.example", Err: context.Canceled}
	if Temporary(err) {
		t.Fatal("classified caller cancellation as a temporary network failure")
	}
}
func TestChallengeMACAndLimits(t *testing.T) {
	for _, badMAC := range []bool{false, true} {
		t.Run(fmt.Sprint("badMAC=", badMAC), func(t *testing.T) {
			challenge, aka := testChallenge(t, badMAC)
			calls, simCalls := 0, 0
			client := NewClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				reqs := readRequests(t, r)
				calls++
				return httpReply(200, fmt.Sprintf(`[{"response-id":%d,"status":6302,"challenge":%q}]`, reqs[0].ID, challenge)), nil
			})})
			_, err := client.Lookup(t.Context(), testProfile(), testIdentity(), func(context.Context, []byte, []byte) (usim.AKAResult, error) { simCalls++; return aka, nil })
			if err == nil {
				t.Fatal("accepted invalid MAC or unbounded challenges")
			}
			if badMAC && (calls != 1 || simCalls != 1) {
				t.Fatal("continued after invalid MAC")
			}
			if !badMAC && (calls != 3 || simCalls != 2) {
				t.Fatal("challenge limit not enforced")
			}
		})
	}
}
func TestHTTPRetryAndPrivacy(t *testing.T) {
	client := NewClient(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		r := httpReply(429, "private_subscriber_data")
		r.Header.Set("Retry-After", "999999999999")
		return r, nil
	})})
	_, err := client.Lookup(t.Context(), testProfile(), testIdentity(), func(context.Context, []byte, []byte) (usim.AKAResult, error) { return usim.AKAResult{}, nil })
	var status *StatusError
	if !errors.As(err, &status) || !Temporary(err) || status.RetryAfter != 24*time.Hour || strings.Contains(err.Error(), "private_") {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, code := range []int{6005, 6050} {
		if !Temporary(&StatusError{Code: code}) {
			t.Fatal("temporary carrier error classified as permanent")
		}
	}
}

func TestPNRUnavailable(t *testing.T) {
	for name, returnedNumber := range map[string]string{"without number": "", "with number": "+15551234567"} {
		t.Run(name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`[{"response-id":1,"status":6000},{"response-id":3,"status":6020,"phone-number":%q}]`, returnedNumber)
				return httpReply(200, body), nil
			})})
			number, err := client.Lookup(t.Context(), testProfile(), testIdentity(), func(context.Context, []byte, []byte) (usim.AKAResult, error) {
				t.Fatal("unexpected SIM access")
				return usim.AKAResult{}, nil
			})
			status, ok := errors.AsType[*StatusError](err)
			if !ok || number != "" || status.Action != "getPhoneNumber" || status.Code != 6020 || Temporary(err) {
				t.Fatalf("PNR unavailable result = %q, %v", number, err)
			}
			if status.NumberPresent != (returnedNumber != "") || !strings.Contains(err.Error(), "PNR not available") {
				t.Fatalf("missing safe PNR diagnostics: %v", err)
			}
			if returnedNumber != "" && strings.Contains(err.Error(), returnedNumber) {
				t.Fatal("error exposed a number from an unsuccessful response")
			}
		})
	}
}
