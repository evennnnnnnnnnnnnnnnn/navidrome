package nativeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

// addLyricsSidecarRoute registers the endpoint that writes a .lrc sidecar next
// to a song's audio file. It must be registered inside the adminOnlyMiddleware
// group: it writes into the library folder, exactly as the YouTube import does.
func (api *Router) addLyricsSidecarRoute(r chi.Router) {
	r.Route("/lyricssidecar/{id}", func(r chi.Router) {
		r.Use(server.URLParamsMiddleware)
		r.Post("/", saveLyricsSidecar(api.ds))
	})
}

type lyricsSidecarRequest struct {
	Content string `json:"content"`
}

// sidecarPathFor derives the .lrc destination for a media file. The path comes
// entirely from the stored record, never from client input, and the result is
// verified to stay inside the library root so a corrupt or hostile Path column
// still cannot escape it.
func sidecarPathFor(mf *model.MediaFile) (string, error) {
	if mf.LibraryPath == "" {
		return "", errors.New("media file has no library path")
	}

	absolute := filepath.Join(mf.LibraryPath, mf.Path)
	ext := filepath.Ext(absolute)
	sidecar := strings.TrimSuffix(absolute, ext) + ".lrc"

	root, err := filepath.Abs(mf.LibraryPath)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(sidecar)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("sidecar path escapes the library: %s", rel)
	}

	return resolved, nil
}

// writeSidecarAtomically writes through a temp file in the destination
// directory and renames it into place. Sidecars are read at request time by
// core/lyrics, so a reader must never observe a half-written file.
func writeSidecarAtomically(path string, contents []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".lyrics-*.lrc")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// No-op once the rename succeeded; cleans up on every failure path.
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func saveLyricsSidecar(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParamFromCtx(ctx, "id")

		var payload lyricsSidecarRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.Content) == "" {
			http.Error(w, "content is required", http.StatusBadRequest)
			return
		}

		mf, err := ds.MediaFile(ctx).Get(id)
		if errors.Is(err, model.ErrNotFound) {
			http.Error(w, "song not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Error(ctx, "Error retrieving song for lyrics sidecar", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Validate before writing, so a bad paste can never leave a broken
		// sidecar in the library. This is the same parser the read path uses.
		list, err := model.ParseLyrics(ctx, ".lrc", "xxx", []byte(payload.Content))
		if err != nil {
			http.Error(w, "could not parse the lyrics: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(list) == 0 || len(list[0].Line) == 0 {
			http.Error(w, "the lyrics contain no lines", http.StatusBadRequest)
			return
		}

		path, err := sidecarPathFor(mf)
		if err != nil {
			log.Error(ctx, "Error resolving lyrics sidecar path", "id", id, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := writeSidecarAtomically(path, []byte(payload.Content)); err != nil {
			log.Error(ctx, "Error writing lyrics sidecar", "id", id, "path", path, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Info(ctx, "Wrote lyrics sidecar", "id", id, "path", path, "lines", len(list[0].Line))
		w.WriteHeader(http.StatusNoContent)
	}
}
