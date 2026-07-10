package server

import (
	"bytes"
	"html/template"

	"nyx-drop/internal/config"
	"nyx-drop/web"
)

// notFoundData is the template data for web/notfound.html.
type notFoundData struct {
	AppURL string
}

// renderNotFound renders web/notfound.html once, injecting cfg's public
// origin as the "deploy a new site" link target. The mockup's hardcoded
// sites.nyxhub.net link is wrong for a self-hosted instance at another
// domain, so the page is templated rather than served as a static byte
// slice. Callers cache the returned bytes (server.New does this at
// startup) rather than re-rendering per request.
func renderNotFound(cfg *config.Config) ([]byte, error) {
	tmpl, err := template.ParseFS(web.Files, "notfound.html")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, notFoundData{AppURL: cfg.ExternalOrigin()}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
