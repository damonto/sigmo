package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func TestContactAPIAndAvatarValidation(t *testing.T) {
	server := echo.New()
	if err := Register(server, newTestConfig(t)); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}
	created := request(http.MethodPost, "/contacts", `{"name":"Name only","numbers":[],"notes":"Private"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d, %s", created.Code, created.Body.String())
	}
	var contact storage.Contact
	if err := json.Unmarshal(created.Body.Bytes(), &contact); err != nil {
		t.Fatal(err)
	}
	if got, want := created.Header().Get("Location"), "/api/v1/contacts/"+contact.ID; got != want {
		t.Fatalf("created Location = %q, want %q", got, want)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/contacts/" + contact.ID, "", 200},
		{http.MethodPut, "/contacts/" + contact.ID + "/avatar", "invalid image", 400},
		{http.MethodGet, "/contacts/" + contact.ID + "/avatar", "", 404},
		{http.MethodPut, "/contacts/" + contact.ID, `{"name":"Updated","numbers":[{"number":"10086","label":"service"}]}`, 200},
		{http.MethodPut, "/sim-profiles/missing/history-assignment", `{"targetProfileId":"missing"}`, 409},
		{http.MethodGet, "/message-threads?cursor=invalid", "", 400},
		{http.MethodDelete, "/contacts/" + contact.ID, "", 204},
		{http.MethodGet, "/contacts/" + contact.ID, "", 404},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := request(tc.method, tc.path, tc.body)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
		})
	}
}
