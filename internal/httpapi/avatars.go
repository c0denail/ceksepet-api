package httpapi

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

type avatar struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Color  string `json:"-"`
	Accent string `json:"-"`
}

var avatarCatalog = []avatar{
	{ID: "avatar_01", Name: "Yeşil", Color: "#16A34A", Accent: "#FACC15"},
	{ID: "avatar_02", Name: "Sarı", Color: "#EAB308", Accent: "#15803D"},
	{ID: "avatar_03", Name: "Mavi", Color: "#2563EB", Accent: "#FACC15"},
	{ID: "avatar_04", Name: "Mor", Color: "#7C3AED", Accent: "#22C55E"},
	{ID: "avatar_05", Name: "Turuncu", Color: "#EA580C", Accent: "#FDE047"},
	{ID: "avatar_06", Name: "Pembe", Color: "#DB2777", Accent: "#86EFAC"},
	{ID: "avatar_07", Name: "Turkuaz", Color: "#0D9488", Accent: "#FDE047"},
	{ID: "avatar_08", Name: "Lacivert", Color: "#1E3A8A", Accent: "#FBBF24"},
}

func (s *Server) listAvatars(w http.ResponseWriter, _ *http.Request) {
	result := make([]avatar, len(avatarCatalog))
	for index, item := range avatarCatalog {
		item.URL = s.avatarURL(item.ID)
		result[index] = item
	}
	writeJSON(w, http.StatusOK, map[string]any{"avatars": result})
}

func (s *Server) serveAvatar(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !strings.HasSuffix(filename, ".svg") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(filename, ".svg")
	item, ok := findAvatar(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" role="img" aria-label="%s avatar">
<rect width="256" height="256" rx="128" fill="%s"/><circle cx="128" cy="102" r="50" fill="#FFF7ED"/>
<path d="M42 234c9-50 40-76 86-76s77 26 86 76" fill="#FFF7ED"/>
<circle cx="108" cy="96" r="6" fill="#1F2937"/><circle cx="148" cy="96" r="6" fill="#1F2937"/>
<path d="M108 124c13 12 27 12 40 0" fill="none" stroke="#1F2937" stroke-width="7" stroke-linecap="round"/>
<circle cx="206" cy="50" r="18" fill="%s"/></svg>`, html.EscapeString(item.Name), item.Color, item.Accent)
}

func findAvatar(id string) (avatar, bool) {
	for _, item := range avatarCatalog {
		if item.ID == id {
			return item, true
		}
	}
	return avatar{}, false
}

func (s *Server) avatarURL(id string) string { return s.cfg.PublicBaseURL + "/avatars/" + id + ".svg" }
