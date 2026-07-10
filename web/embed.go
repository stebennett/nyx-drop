// Package web embeds the frontend assets served by internal/server. Go's
// //go:embed directive cannot reach parent directories, so the directive
// must live in this package, not internal/server; later cards add files
// to this embed set (upload page, admin UI, /static/* assets).
package web

import "embed"

//go:embed notfound.html
var Files embed.FS
