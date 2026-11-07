package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"time"
)

//go:embed templates/*.html static/*
var embedded embed.FS

var (
	baseHTML   = mustRead("templates/base.html")
	indexHTML  = mustRead("templates/index.html")
	repoHTML   = mustRead("templates/repo.html")
	errorHTML  = mustRead("templates/error.html")
	webCSS     = mustRead("static/web.css")
	faviconSVG = mustRead("static/favicon.svg")
)

func mustRead(name string) []byte {
	b, err := embedded.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}

func serveAsset(w http.ResponseWriter, r *http.Request, name, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", assetETag(data))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func assetETag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}
