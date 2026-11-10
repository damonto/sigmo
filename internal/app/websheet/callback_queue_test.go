package websheet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"testing"
)

func mustQueueCallback(t *testing.T, session *Session, callback Callback) {
	t.Helper()
	if err := session.Callback(callback); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackEndpointRejectsOverflowWithoutLosingEvents(t *testing.T) {
	broker := New(Config{})
	session, err := broker.Create(t.Context(), Request{URL: "https://example.com/portal"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range maxPendingCallbacks {
		mustQueueCallback(t, session, Callback{ID: fmt.Sprint(i), Event: "checkProfileServiceStatus"})
	}
	// A retry of an accepted event remains successful even when the queue is full.
	mustQueueCallback(t, session, Callback{ID: "0", Event: "checkProfileServiceStatus"})
	e := echo.New()
	RegisterRoutes(e.Group("/api/v1"), broker)
	post := func(wantStatus int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, session.Info().EmbedURL+"/callback", strings.NewReader(`{"callbackId":"retry","event":"finishFlow"}`))
		request.Header.Set("Origin", "null")
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, request)
		if recorder.Code != wantStatus || recorder.Header().Get("Access-Control-Allow-Origin") != "null" {
			t.Fatalf("response=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body)
		}
	}
	post(http.StatusTooManyRequests)
	for i := range maxPendingCallbacks {
		callback, err := session.WaitCallback(t.Context())
		if err != nil || callback.ID != fmt.Sprint(i) {
			t.Fatalf("callback=%+v err=%v", callback, err)
		}
	}
	post(http.StatusNoContent)
	callback, err := session.WaitCallback(t.Context())
	if err != nil || callback.ID != "retry" || callback.Event != "finishFlow" {
		t.Fatalf("retried callback=%+v err=%v", callback, err)
	}
}

func TestCallbackDeduplicationHistoryIsBounded(t *testing.T) {
	session, err := New(Config{}).Create(t.Context(), Request{URL: "https://example.com/portal"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range maxRecentCallbackIDs + 1 {
		mustQueueCallback(t, session, Callback{ID: fmt.Sprint(i), Event: "checkProfileServiceStatus"})
		if _, err := session.WaitCallback(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	// The oldest delivery can be accepted again; a recent retry is still ignored.
	mustQueueCallback(t, session, Callback{ID: fmt.Sprint(maxRecentCallbackIDs), Event: "checkProfileServiceStatus"})
	mustQueueCallback(t, session, Callback{ID: "0", Event: "checkProfileServiceStatus"})
	callback, err := session.WaitCallback(t.Context())
	if err != nil || callback.ID != "0" {
		t.Fatalf("callback=%+v err=%v", callback, err)
	}
}

func TestCallbackQueuePreservesEvents(t *testing.T) {
	session, err := New(Config{}).Create(t.Context(), Request{URL: "https://example.com/portal"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"deleteToken", "profileReadyWithActivationCode", "finishFlow"} {
		mustQueueCallback(t, session, Callback{Event: event})
	}
	for _, event := range []string{"deleteToken", "profileReadyWithActivationCode", "finishFlow"} {
		callback, err := session.WaitCallback(t.Context())
		if err != nil || callback.Event != event {
			t.Fatalf("callback=%+v err=%v, want %s", callback, err, event)
		}
	}
}

func TestCallbackQueueDeduplicatesDeliveryIDs(t *testing.T) {
	session, err := New(Config{}).Create(t.Context(), Request{URL: "https://example.com/portal"})
	if err != nil {
		t.Fatal(err)
	}
	mustQueueCallback(t, session, Callback{ID: "1", Event: "checkProfileServiceStatus"})
	mustQueueCallback(t, session, Callback{ID: "1", Event: "checkProfileServiceStatus"})
	mustQueueCallback(t, session, Callback{ID: "2", Event: "checkProfileServiceStatus"})
	for _, id := range []string{"1", "2"} {
		callback, err := session.WaitCallback(t.Context())
		if err != nil || callback.ID != id {
			t.Fatalf("callback=%+v err=%v", callback, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := session.WaitCallback(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
}

func TestCallbackQueueConcurrentDelivery(t *testing.T) {
	session, err := New(Config{}).Create(t.Context(), Request{URL: "https://example.com/portal"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var producers sync.WaitGroup
	for i := range 50 {
		producers.Go(func() {
			callback := Callback{ID: fmt.Sprint(i), Event: "checkProfileServiceStatus"}
			for range 2 {
				if err := session.Callback(callback); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	seen := make(map[string]bool)
	for range 50 {
		callback, err := session.WaitCallback(ctx)
		if err != nil || seen[callback.ID] {
			t.Fatalf("callback=%+v err=%v", callback, err)
		}
		seen[callback.ID] = true
	}
	producers.Wait()
}
