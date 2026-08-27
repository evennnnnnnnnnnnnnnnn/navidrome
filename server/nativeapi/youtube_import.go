package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/ytimport"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
)

// newYtImporter and callScan are swapped by tests to avoid spawning a real
// yt-dlp or a real library scan.
var (
	newYtImporter = ytimport.New
	callScan      = scanner.CallScan
)

// addYoutubeImportRoute registers the YouTube import endpoint. It must be
// registered inside the adminOnlyMiddleware group: the pipeline writes files
// into the library folder, so it is admin-only by design.
func (api *Router) addYoutubeImportRoute(r chi.Router) {
	r.Post("/youtubeimport", api.handleYoutubeImport)
}

type youtubeImportRequest struct {
	URL       string `json:"url"`
	LibraryID int    `json:"libraryId"`
}

type youtubeImportResponse struct {
	ytimport.Result
	ScanTriggered bool `json:"scanTriggered"`
}

func (api *Router) handleYoutubeImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload youtubeImportRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if payload.LibraryID == 0 {
		payload.LibraryID = model.DefaultLibraryID
	}

	result, err := newYtImporter(api.ds).Import(ctx, payload.URL, payload.LibraryID)
	var downloadErr *ytimport.DownloadFailedError
	switch {
	case errors.Is(err, ytimport.ErrYtdlpNotFound):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	case errors.As(err, &downloadErr):
		http.Error(w, downloadErr.Error(), http.StatusUnprocessableEntity)
		return
	case err != nil:
		log.Error(ctx, "YouTube import failed", "url", payload.URL, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := youtubeImportResponse{Result: *result}
	resp.ScanTriggered = api.triggerYtImportScan(ctx, payload.LibraryID)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(ctx, "Error encoding YouTube import response", err)
	}
}

// triggerYtImportScan scans the YouTube/ subfolder so the imported track (and
// its .lrc sidecar) appears in the library right away. Best-effort: when a
// scan is already running, the periodic scan picks the file up instead. The
// scan is detached from the request context so a client disconnect cannot
// abort it mid-write.
func (api *Router) triggerYtImportScan(ctx context.Context, libraryID int) bool {
	scanCtx := context.WithoutCancel(ctx)
	targets := []model.ScanTarget{{LibraryID: libraryID, FolderPath: ytimport.Subfolder}}
	progress, err := callScan(scanCtx, api.ds, api.playlists, false, targets)
	if err != nil {
		if errors.Is(err, scanner.ErrAlreadyScanning) {
			log.Info(ctx, "YouTube import: scan already running, imported file will be picked up later")
		} else {
			log.Error(ctx, "YouTube import: triggering scan failed", err)
		}
		return false
	}
	// Drain until the scan finishes so the response means "in the library".
	for range progress {
	}
	return true
}
