package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPIYAML []byte

func (s *Server) openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(openAPIYAML)
}

func (s *Server) swaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="tr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>ÇekSepet Mobile API — Swagger</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.28.1/swagger-ui.css">
  <style>body{margin:0;background:#fafafa}.topbar{display:none}</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.28.1/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => SwaggerUIBundle({
      url: '/swagger/openapi.yaml',
      dom_id: '#swagger-ui',
      deepLinking: true,
      displayRequestDuration: true,
      persistAuthorization: true,
      tryItOutEnabled: true,
      layout: 'BaseLayout'
    });
  </script>
</body>
</html>`))
}
