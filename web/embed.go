package web

import "embed"

//go:embed templates/*.html static/*.css vendor/*.js
var FS embed.FS
