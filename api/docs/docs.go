// Package docs embeds the OpenAPI spec and serves the Scalar API reference.
package docs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var spec []byte

// SpecHandler serves the raw OpenAPI YAML spec.
func SpecHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(spec) //nolint:errcheck
	}
}

// ScalarHandler serves an HTML page that renders the API docs with Scalar.
// It overrides the global CSP to allow the Scalar CDN scripts and styles.
func ScalarHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
				"style-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
				"font-src 'self' https://cdn.jsdelivr.net https://fonts.scalar.com data:; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self'; "+
				"worker-src blob:")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(scalarHTML)) //nolint:errcheck
	}
}

const scalarHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Taillight API</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</head>
<body>
  <script id="api-reference" data-url="/api/v1/openapi.yaml"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`
