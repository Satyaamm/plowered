package http

import (
	_ "embed"
	"net/http"
)

// Swagger UI is served as a self-contained HTML page that pulls the
// upstream `swagger-ui-dist@5` assets from a CDN. We don't bundle them
// to keep the binary small; users running fully offline can swap the
// CDN URLs for a local mirror without a code change.

//go:embed openapi.yaml
var openapiYAML []byte

const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Plowered API · Swagger UI</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  <link rel="icon" type="image/png" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/favicon-32x32.png" sizes="32x32" />
  <style>
    body { margin: 0; background: #fafafa; }
    .swagger-ui .topbar { background: #2C1F12; }
    .swagger-ui .topbar .download-url-wrapper { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js" crossorigin></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: "/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        layout: "StandaloneLayout",
        // Use HTTP cookie session for in-browser try-it-out so existing
        // logins flow through without re-auth in the docs page.
        withCredentials: true,
        requestInterceptor: (req) => {
          req.credentials = "include";
          return req;
        },
      });
    };
  </script>
</body>
</html>
`

// DocsHandlers mounts the OpenAPI spec at /openapi.yaml and Swagger UI
// at /docs. Both are public — the spec describes auth requirements
// inline, so anyone who reaches /docs can read it without logging in.
func DocsHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(openapiYAML)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte(swaggerHTML))
	})
	mux.HandleFunc("GET /docs/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	})
}
