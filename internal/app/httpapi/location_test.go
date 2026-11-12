package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestResourceLocationPreservesEscapedParentsAndEncodesMemberID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/prefix/api/v1/modems/one%2Ftwo/calls?private=value", nil)
	rec := httptest.NewRecorder()
	SetResourceLocation(echo.New().NewContext(req, rec), "call/with ?")
	if got, want := rec.Header().Get("Location"), "/prefix/api/v1/modems/one%2Ftwo/calls/call%2Fwith%20%3F"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}
