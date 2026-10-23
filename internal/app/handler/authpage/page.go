// Package authpage renders the terminal browser page shared by OIDC test flows.
package authpage

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/locale"
)

// Write renders a minimal result page without scripts, external assets, or credentials.
func Write(c *echo.Context, succeeded bool, language locale.Tag) error {
	title, message := "OIDC test", "The OIDC test did not complete. Return to Sigmo for details."
	if succeeded {
		title, message = "OIDC test succeeded", "The OIDC test succeeded. Return to Sigmo to continue."
	}
	pageLanguage := "en"
	if language == locale.Chinese {
		title, message, pageLanguage = "OIDC 测试", "OIDC 测试未完成，请返回 Sigmo 查看详情。", "zh-Hans"
		if succeeded {
			title, message = "OIDC 测试成功", "OIDC 测试成功，请返回 Sigmo 继续操作。"
		}
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	c.Response().Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	page := fmt.Sprintf(`<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><body style="font:16px system-ui,sans-serif;max-width:36rem;margin:12vh auto;padding:0 1.5rem;line-height:1.6"><h1>%s</h1><p>%s</p></body></html>`,
		pageLanguage, title, title, message)
	return c.HTML(http.StatusOK, page)
}
