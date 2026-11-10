package esimtransfer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/damonto/ts43-go"

	"github.com/damonto/sigmo/internal/app/websheet"
)

func TestWebsheetDeleteTokenKeepsCarrierSession(t *testing.T) {
	continued := make(chan int, 1)
	authentications := 0
	client, err := ts43.New(&ts43.Config{
		Logger:      slog.New(slog.DiscardHandler),
		Entitlement: ts43.Entitlement{Server: "https://carrier.example"},
		Source:      ts43.Endpoint{Channel: &transferTestChannel{identity: &ts43.Identity{MCC: "234", MNC: "20", IMSI: "234201234567890"}}, Device: ts43.Device{IMEI: "111111111111111"}},
		Target:      ts43.Endpoint{Device: ts43.Device{IMEI: "222222222222222", EID: "89049032000001000000000000000000"}},
		HTTPClient: transferTestDoer(func(req *http.Request) (*http.Response, error) {
			var body string
			switch ts43.Operation(req.URL.Query().Get("operation")) {
			case "":
				authentications++
				body = `{"TOKEN":{"token":"auth","Validity":3600}}`
			case ts43.OperationCheck:
				body = `{"APPLICATION":{"OperationResult":1,"PrimaryAppEligibility":1}}`
			case ts43.OperationManage:
				body = `{"APPLICATION":{"OperationResult":1,"SubscriptionResult":1,"SubscriptionServiceURL":"http://127.0.0.1/carrier"}}`
			case ts43.OperationAcquireConf:
				continued <- authentications
				body = `{"APPLICATION":{"PrimaryConfiguration":{"ServiceStatus":2,"PollingInterval":0}}}`
			default:
				t.Errorf("unexpected carrier operation %q", req.URL.Query().Get("operation"))
				return nil, errors.New("unexpected carrier operation")
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	result, err := client.Transfer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	event, ok := result.Event.(ts43.WebsheetEvent)
	if !ok {
		t.Fatalf("event = %T", result.Event)
	}
	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	finished := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := testWSUpgrader.Upgrade(w, req, nil)
		if err != nil {
			finished <- err
			return
		}
		session := newWSSession(ctx, conn, cancel)
		defer session.close()
		runner := &transferRunner{websheets: broker}
		_, err = runner.handleWebsheet(ctx, session, &transferState{ts43Client: client}, result, event)
		finished <- err
	}))
	t.Cleanup(server.Close)
	conn := dialTestWebSocket(t, server.URL)
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message wsServerMessage
	for message.Type != wsTypeWebsheet {
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
	}
	if message.Websheet == nil {
		t.Fatal("websheet information is missing")
	}
	page, err := broker.Get(message.Websheet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Callback(websheet.Callback{Event: "deleteToken"}); err != nil {
		t.Fatal(err)
	}
	if err := page.Callback(websheet.Callback{Event: "checkProfileServiceStatus"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		t.Fatalf("deleteToken closed the carrier session: %v", err)
	case count := <-continued:
		if count != 2 {
			t.Fatalf("authentications = %d, want initial authentication and refresh after deleteToken", count)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("status callback did not continue the same carrier session")
	}
	if current, err := broker.Get(message.Websheet.ID); err != nil || current != page {
		t.Fatalf("carrier session was replaced: %v", err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("session cancellation = %v", err)
	}
}
