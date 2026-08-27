package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

// addLyricsOverrideRoute registers the admin-edited shared-lyrics endpoints. Reads
// are open to any authenticated user; writes are gated by the repository's
// isPermitted() check (admin only).
func (api *Router) addLyricsOverrideRoute(r chi.Router) {
	r.Route("/lyricsoverride/{id}", func(r chi.Router) {
		r.Use(server.URLParamsMiddleware)
		r.Get("/", getLyricsOverride(api.ds))
		r.Put("/", saveLyricsOverride(api.ds))
		r.Post("/", saveLyricsOverride(api.ds))
		r.Delete("/", deleteLyricsOverride(api.ds))
	})
}

func getLyricsOverride(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParamFromCtx(ctx, "id")

		override, err := ds.LyricsOverride(ctx).Get(id)
		if errors.Is(err, model.ErrNotFound) {
			http.Error(w, "lyrics override not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Error(ctx, "Error retrieving lyrics override", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		list, err := override.StructuredLyrics()
		if err != nil {
			log.Error(ctx, "Error decoding lyrics override", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(list)
		if err != nil {
			log.Error(ctx, "Error marshaling lyrics override", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp) //nolint:gosec
	}
}

func saveLyricsOverride(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParamFromCtx(ctx, "id")

		var list model.LyricList
		if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := ds.LyricsOverride(ctx).Put(id, list)
		if errors.Is(err, rest.ErrPermissionDenied) {
			http.Error(w, "admin privileges required", http.StatusForbidden)
			return
		}
		if err != nil {
			log.Error(ctx, "Error saving lyrics override", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func deleteLyricsOverride(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParamFromCtx(ctx, "id")

		err := ds.LyricsOverride(ctx).Delete(id)
		if errors.Is(err, rest.ErrPermissionDenied) {
			http.Error(w, "admin privileges required", http.StatusForbidden)
			return
		}
		if err != nil {
			log.Error(ctx, "Error deleting lyrics override", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
