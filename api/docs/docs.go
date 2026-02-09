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
  <style>
    body { margin: 0; }
    .tl-header {
      display: flex;
      align-items: center;
      gap: 1rem;
      padding: 0.5rem 1rem;
      background: #16161e;
      border-bottom: 1px solid #292e42;
      font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, monospace;
      font-size: 0.75rem;
    }
    .tl-logo {
      font-weight: 600;
      background: linear-gradient(to right, #ff007c, #c026d3);
      -webkit-background-clip: text;
      background-clip: text;
      color: transparent;
      text-decoration: none;
    }
    .tl-logo:hover span { text-decoration: underline; }
    .tl-back {
      color: #565f89;
      text-decoration: none;
      transition: color 0.15s;
    }
    .tl-back:hover { color: #7aa2f7; }
  </style>
</head>
<body>
  <div class="tl-header">
    <a href="/" class="tl-logo">[<span>Taillight</span>]</a>
    <a href="/" class="tl-back">&#8592; back to app</a>
  </div>
  <script
    id="api-reference"
    data-url="/api/v1/openapi.yaml"
    data-configuration='{"darkMode":true,"hideDarkModeToggle":true}'
  ></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`
