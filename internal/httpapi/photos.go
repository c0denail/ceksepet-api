package httpapi

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

func (s *Server) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeError(w, 400, *validationError("file", "En fazla 5 MB JPEG/PNG dosyası gönderin."))
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		writeError(w, 400, *validationError("file", "file alanında tek bir fotoğraf gereklidir."))
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, *validationError("file", "Fotoğraf gerekli."))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (5<<20)+1))
	if err != nil || len(data) > 5<<20 {
		writeError(w, 400, *validationError("file", "Fotoğraf en fazla 5 MB olabilir."))
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width <= 0 || config.Height <= 0 || config.Width > 8000 || config.Height > 8000 || int64(config.Width)*int64(config.Height) > 20_000_000 {
		writeError(w, 400, *validationError("file", "Geçerli JPEG/PNG gerekli; en fazla 8000 piksel kenar ve 20 megapiksel."))
		return
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		writeError(w, 400, *validationError("file", "Fotoğraf bozuk veya eksik."))
		return
	}
	photo := domain.Photo{ID: security.NewID(), OwnerID: user, ContentType: "image/" + format, Data: data, CreatedAt: s.now().UTC()}
	if err := s.store.SavePhoto(r.Context(), photo); err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": photo.ID, "url": s.cfg.PublicBaseURL + "/api/v1/photos/" + photo.ID, "contentType": photo.ContentType, "size": len(data), "width": config.Width, "height": config.Height})
}

func (s *Server) servePhoto(w http.ResponseWriter, r *http.Request) {
	user := ""
	operator := false
	if r.Header.Get("Authorization") != "" {
		_, id, e := s.authenticate(r)
		if e == nil {
			user = id
		} else {
			if _, _, oe := s.authenticateOperator(r); oe != nil {
				writeError(w, 401, *e)
				return
			}
			operator = true
		}
	}
	photo, err := s.store.GetPhoto(r.Context(), r.PathValue("photoId"))
	if err != nil {
		s.marketError(w, err)
		return
	}
	public := false
	if photo.RaffleID != "" {
		raffle, err := s.store.GetRaffle(r.Context(), photo.RaffleID)
		if err != nil {
			s.marketError(w, err)
			return
		}
		public = raffle.Public()
	}
	if !public && user != photo.OwnerID && !operator {
		s.marketError(w, store.ErrNotFound)
		return
	}
	w.Header().Set("Content-Type", photo.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, photo.ID, photo.CreatedAt, bytes.NewReader(photo.Data))
}
