package communication

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func TestContactAPIAndAvatarValidation(t *testing.T) {
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "communication.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	server := echo.New()
	New(Config{Storage: store}).Register(server.Group(""))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
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
