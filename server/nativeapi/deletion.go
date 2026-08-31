package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/req"
)

// addDeletionRoute registers the admin-only endpoints that delete media from the library.
// It must stay inside the adminOnlyMiddleware group: these handlers move files out of the
// music folder, the same reason the YouTube import and the lyrics sidecar live there.
//
// Unlike /missing, an empty id list is an error rather than "delete everything" - there is
// no bulk verb here on purpose.
func (api *Router) addDeletionRoute(r chi.Router) {
	r.Route("/deletion", func(r chi.Router) {
		r.Delete("/song", deleteSongs(api.maintenance))
		r.Delete("/album", deleteAlbums(api.maintenance))
	})
}

func deleteSongs(maintenance core.Maintenance) http.HandlerFunc {
	return deletionHandler("song", func(r *http.Request, ids []string) (*core.DeletionResult, error) {
		return maintenance.DeleteMediaFiles(r.Context(), ids)
	})
}

func deleteAlbums(maintenance core.Maintenance) http.HandlerFunc {
	return deletionHandler("album", func(r *http.Request, ids []string) (*core.DeletionResult, error) {
		return maintenance.DeleteAlbums(r.Context(), ids)
	})
}

func deletionHandler(kind string, delete func(*http.Request, []string) (*core.DeletionResult, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ids := req.Params(r).Strings("id")

		result, err := delete(r, ids)
		if err != nil {
			writeDeletionError(w, r, kind, ids, err)
			return
		}

		log.Info(ctx, "Deleted from library", "kind", kind, "requested", ids, "deleted", result.Count)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Error(ctx, "Error encoding deletion response", err)
		}
	}
}

func writeDeletionError(w http.ResponseWriter, r *http.Request, kind string, ids []string, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, core.ErrNoIDs):
		writeError(w, ctx, http.StatusBadRequest, "no ids given")
	case errors.Is(err, core.ErrDeletionDisabled):
		writeError(w, ctx, http.StatusForbidden, "deleting media files is disabled on this server")
	case errors.Is(err, core.ErrDeletionNotAdmin):
		writeError(w, ctx, http.StatusForbidden, "Access denied: admin privileges required")
	case errors.Is(err, model.ErrNotFound):
		writeError(w, ctx, http.StatusNotFound, "not found")
	case errors.Is(err, core.ErrUnsafeDeletion):
		// Nothing was moved: the request named a path the server will not touch.
		log.Warn(ctx, "Refused an unsafe delete request", "kind", kind, "ids", ids, err)
		writeError(w, ctx, http.StatusBadRequest, err.Error())
	default:
		// Includes the partial-batch case, where some files were deleted before the run
		// stopped. The message carries the counts and the trash folder, so say it out loud
		// rather than hiding it behind a generic failure.
		log.Error(ctx, "Error deleting from library", "kind", kind, "ids", ids, err)
		writeError(w, ctx, http.StatusInternalServerError, err.Error())
	}
}

// writeError answers with a JSON body, because the web UI reads `message` off the parsed
// response; a plain-text body would leave it showing only the HTTP status text.
func writeError(w http.ResponseWriter, ctx context.Context, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"message": message}); err != nil {
		log.Error(ctx, "Error encoding deletion error response", err)
	}
}
