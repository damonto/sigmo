package httpapi

import (
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/locale"
)

// LocaleHeader carries the language the web UI is shown in. The UI sends it
// on every API request because only the UI knows which of the browser's
// languages it picked.
const LocaleHeader = "X-Sigmo-Locale"

// RequestLocale is the web UI language of the request, or the zero Tag when
// the request did not come from the web UI.
func RequestLocale(c *echo.Context) locale.Tag {
	return locale.Parse(c.Request().Header.Get(LocaleHeader))
}
