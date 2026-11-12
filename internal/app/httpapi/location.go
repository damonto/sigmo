package httpapi

import (
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
)

// SetResourceLocation identifies a member created under the requested collection.
// EscapedPath preserves encoded identifiers and reverse-proxy prefixes.
func SetResourceLocation(c *echo.Context, id string) {
	c.Response().Header().Set("Location", strings.TrimRight(c.Request().URL.EscapedPath(), "/")+"/"+url.PathEscape(id))
}
