// Package docs serves the API's OpenAPI specification and a human-readable
// reference page. The spec is embedded in the binary so it ships with the
// service regardless of working directory (same approach as migrations).
package docs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPISpec []byte

// SpecYAML serves the raw OpenAPI document. Tooling (Redoc, Swagger UI,
// Postman, code generators) can consume it directly at GET /openapi.yaml.
func SpecYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openAPISpec)
}

// Reference serves a self-contained Redoc page rendering the spec. GET /docs.
// Redoc is loaded from a CDN — acceptable here because this is a documentation
// page served by our own API, not a sandboxed artifact.
func Reference(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(referencePage))
}

const referencePage = `<!doctype html>
<html>
  <head>
    <title>MasterFabric Raw LLM Monitoring — API Reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>body { margin: 0; padding: 0; }</style>
  </head>
  <body>
    <redoc spec-url="/openapi.yaml"></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
  </body>
</html>`
