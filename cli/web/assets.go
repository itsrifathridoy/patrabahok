// Package web embeds the admin dashboard's templates and static assets directly into
// the compiled binary, so the deployed server is a single self-contained executable
// with no runtime dependency on files being present on disk alongside it.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sort"
)

//go:embed templates/*.html
var TemplatesFS embed.FS

//go:embed static/*
var StaticFS embed.FS

// AssetVersion is a short hash of every embedded static file's content, computed once
// at startup. Static assets are served with a long Cache-Control (see
// internal/webui/server.go's cacheHeaders), which is only safe because their URLs
// include this version — a browser that cached the old CSS/JS under the old version
// string never gets served new content under a URL it hasn't cached, instead of
// silently rendering new HTML against a stale stylesheet until the cache expires (the
// exact bug a dashboard redesign hit: browsers kept the hour-old CSS after the new
// markup shipped).
var AssetVersion = computeAssetVersion()

func computeAssetVersion() string {
	var names []string
	_ = fs.WalkDir(StaticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, path)
		}
		return nil
	})
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		data, err := fs.ReadFile(StaticFS, name)
		if err != nil {
			continue
		}
		h.Write([]byte(name))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}
