package Services

import (
	"net/url"
	"os"
	"regexp"
	"strings"
)

var imageProxyBase = func() string {
	if v := os.Getenv("IMAGE_PROXY_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "/api/image/proxy"
}()

func WrapImageURL(rawURL string) string {
	if rawURL == "" || strings.Contains(rawURL, "/image/proxy") {
		return rawURL
	}
	return imageProxyBase + "?url=" + url.QueryEscape(rawURL)
}

func WrapImageURLPtr(u *string, useProxy bool) *string {
	if !useProxy || u == nil || *u == "" {
		return u
	}
	wrapped := WrapImageURL(*u)
	return &wrapped
}

var imgSrcRe = regexp.MustCompile(`(<img\b[^>]*?\bsrc=")([^"]+)(")`)

func ApplyImageProxyToHTML(html string, useProxy bool) string {
	if !useProxy || html == "" {
		return html
	}
	return imgSrcRe.ReplaceAllStringFunc(html, func(match string) string {
		parts := imgSrcRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		return parts[1] + WrapImageURL(parts[2]) + parts[3]
	})
}

func GetUseImageProxy(db DBExecutor) bool {
	var val string
	db.QueryRow("SELECT setting_value FROM global_settings WHERE setting_name = 'use_image_proxy'").Scan(&val)
	return val == "y"
}
