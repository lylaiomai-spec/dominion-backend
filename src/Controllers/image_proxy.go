package Controllers

import (
	"bytes"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
)

func ProxyImage(c *gin.Context, db *sql.DB) {
	if val, _ := Services.GetGlobalSetting("use_image_uploading", db); val != "y" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Image uploading is disabled"})
		c.Abort()
		return
	}
	if val, _ := Services.GetGlobalSetting("use_image_proxy", db); val != "y" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Image proxy is disabled"})
		c.Abort()
		return
	}

	imageURL := c.Query("url")
	if imageURL == "" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "url parameter is required"})
		c.Abort()
		return
	}
	sizeParam := c.Query("size")

	width, height := 0, 0
	if sizeParam != "" {
		parts := strings.SplitN(sizeParam, "x", 2)
		if len(parts) == 2 {
			width, _ = strconv.Atoi(parts[0])
			height, _ = strconv.Atoi(parts[1])
		}
	}

	cacheKey := Services.ProxyCacheKey(imageURL, sizeParam)

	if Services.ImageProxyCache != nil {
		if data, ok := Services.ImageProxyCache.Get(cacheKey); ok {
			writeProxiedImage(c, data)
			return
		}
	}

	resp, err := http.Get(imageURL)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadGateway, Message: "Failed to fetch image: " + err.Error()})
		c.Abort()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadGateway, Message: fmt.Sprintf("Image host returned %d", resp.StatusCode)})
		c.Abort()
		return
	}

	rawData, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to read image"})
		c.Abort()
		return
	}

	src, err := imaging.Decode(bytes.NewReader(rawData))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to decode image"})
		c.Abort()
		return
	}

	resized := src
	if width > 0 || height > 0 {
		resized = imaging.Resize(src, width, height, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 90}); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to encode image"})
		c.Abort()
		return
	}
	result := buf.Bytes()

	if Services.ImageProxyCache != nil {
		maxSizeMB := int64(0)
		if val, err := Services.GetGlobalSetting("proxy_cache_size", db); err == nil {
			maxSizeMB, _ = strconv.ParseInt(val, 10, 64)
		}
		Services.ImageProxyCache.Set(cacheKey, result, maxSizeMB)
	}

	writeProxiedImage(c, result)
}

func writeProxiedImage(c *gin.Context, data []byte) {
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Expires", time.Now().UTC().Add(24*time.Hour).Format(http.TimeFormat))
	c.Data(http.StatusOK, "image/jpeg", data)
}
