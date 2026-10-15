package message

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/apierror"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func TestConversationUpdateRejectsInvalidJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		mediaType  string
		wantStatus int
	}{
		{name: "malformed", body: `{`, mediaType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "trailing document", body: `{"markedUnread":true} {}`, mediaType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "unsupported media", body: `{"markedUnread":true}`, mediaType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "invalid field", body: `{"markedUnread":"private-value"}`, mediaType: "application/json", wantStatus: http.StatusUnprocessableEntity},
		{name: "null", body: `null`, mediaType: "application/json", wantStatus: http.StatusUnprocessableEntity},
		{name: "oversize", body: `{"markedUnread":true}` + strings.Repeat(" ", apierror.MaxBodySize), mediaType: "application/json", wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			e.HTTPErrorHandler = httpapi.HTTPErrorHandler
			// A rejected request must return before using any database dependency.
			e.PATCH("/conversations/:conversationId", New(nil, nil, nil).UpdateConversation)
			req := httptest.NewRequest(http.MethodPatch, "/conversations/thread", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.mediaType)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), apierror.MediaType) {
				t.Fatalf("Content-Type = %q, want Problem Details", rec.Header().Get("Content-Type"))
			}
			if strings.Contains(rec.Body.String(), "private-value") {
				t.Fatal("parser response exposed request data")
			}
		})
	}
}

func TestConversationMessagesUsesDefaultPageWithoutQueryParameters(t *testing.T) {
	s, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	stamp := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for i := range 51 {
		if _, err := s.InsertMessage(t.Context(), storage.Message{ProfileID: "inactive", Source: storage.MessageSourceModem,
			ExternalKey: fmt.Sprint(i), Sender: "10086", Text: fmt.Sprint(i), Timestamp: stamp.Add(time.Duration(i) * time.Second), Incoming: true}); err != nil {
			t.Fatal(err)
		}
	}
	id := storage.ConversationID("inactive", "10086")
	e := echo.New()
	e.GET("/conversations/:conversationId/messages", New(nil, s, nil).ConversationMessages)
	read := func(cursor string) (items []MessageResponse, next string) {
		t.Helper()
		res := httptest.NewRecorder()
		path := "/conversations/" + id + "/messages"
		if cursor != "" {
			path += "?cursor=" + cursor
		}
		e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		var page struct {
			storage.ConversationMessagePage
			Items []MessageResponse `json:"items"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil || res.Code != http.StatusOK || page.Conversation.ID != id {
			t.Fatalf("read page = %d %s, %v", res.Code, res.Body, err)
		}
		return page.Items, page.NextCursor
	}
	first, cursor := read("")
	if len(first) != 50 || first[0].Text != "1" || first[49].Text != "50" || cursor == "" {
		t.Fatalf("default page = %+v, cursor = %q", first, cursor)
	}
	last, cursor := read(cursor)
	if len(last) != 1 || last[0].Text != "0" || cursor != "" {
		t.Fatalf("older page = %+v, cursor = %q", last, cursor)
	}
}

func TestArchivedConversationHTTPContract(t *testing.T) {
	s, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := s.InsertMessage(t.Context(), storage.Message{ProfileID: "inactive", Source: storage.MessageSourceModem, ExternalKey: "one", Sender: "10086", Text: "hello", Timestamp: time.Now(), Incoming: true}); err != nil {
		t.Fatal(err)
	}
	h := New(nil, s, nil)
	e := echo.New()
	e.GET("/conversations", h.Conversations)
	e.GET("/conversations/:conversationId/messages", h.ConversationMessages)
	e.PATCH("/conversations/:conversationId", h.UpdateConversation)
	e.DELETE("/conversations/:conversationId", h.DeleteConversation)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		e.ServeHTTP(res, req)
		return res
	}
	res := request(http.MethodGet, "/conversations?unassigned=true", "")
	if res.Code != 200 {
		t.Fatalf("list = %d: %s", res.Code, res.Body.String())
	}
	var list ConversationsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.BackendID == "" || list.UnreadCount != 1 || list.Items[0].CanSend {
		t.Fatalf("history = %+v", list)
	}
	id := list.Items[0].ID
	for _, query := range []string{"", "?limit=50"} {
		paged := request(http.MethodGet, "/conversations/"+id+"/messages"+query, "")
		var page struct {
			storage.ConversationMessagePage
			Items []MessageResponse `json:"items"`
		}
		if err := json.Unmarshal(paged.Body.Bytes(), &page); err != nil || paged.Code != 200 || len(page.Items) != 1 || page.Conversation.ID != id || page.BackendID == "" {
			t.Fatalf("paged contract %q = %s, %v", query, paged.Body, err)
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=invalid", "limit=50&cursor=invalid"} {
		if res := request(http.MethodGet, "/conversations/"+id+"/messages?"+query, ""); res.Code != 400 {
			t.Fatalf("invalid pagination %s = %d: %s", query, res.Code, res.Body)
		}
	}
	if res := request(http.MethodPatch, "/conversations/"+id, `{"readThroughMessageId":999}`); res.Code != 400 {
		t.Fatalf("foreign watermark=%d", res.Code)
	}
	if res := request(http.MethodPatch, "/conversations/"+id, `{"readThroughMessageId":1,"markedUnread":false}`); res.Code != 200 {
		t.Fatalf("mark read=%d: %s", res.Code, res.Body.String())
	}
	if res := request(http.MethodDelete, "/conversations/"+id, ""); res.Code != 204 {
		t.Fatalf("delete=%d", res.Code)
	}
	if res := request(http.MethodGet, "/conversations/"+id+"/messages", ""); res.Code != 404 {
		t.Fatalf("deleted history=%d", res.Code)
	}
}
