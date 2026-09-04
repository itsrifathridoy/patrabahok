// Package web embeds the admin dashboard's templates and static assets directly into
// the compiled binary, so the deployed server is a single self-contained executable
// with no runtime dependency on files being present on disk alongside it.
package web

import "embed"

//go:embed templates/*.html
var TemplatesFS embed.FS

//go:embed static/*
var StaticFS embed.FS
