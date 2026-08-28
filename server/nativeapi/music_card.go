package nativeapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/ffmpeg"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// clipPaddingMs is the lead-in/lead-out padding added around the requested [start_ms, end_ms]
// window, clamped to the track's own bounds.
const clipPaddingMs = 300

// addMusicCardClipRoute registers the Music Card audio clip endpoint. Any authenticated user may
// request a clip for any media file - clip extraction reads the source audio but writes nothing,
// so it carries none of the per-user ownership rules that guard music_card/music_card_snippet rows.
func (api *Router) addMusicCardClipRoute(r chi.Router) {
	r.Get("/musiccard/clip", getMusicCardClip(api.ds, api.ff))
}

func getMusicCardClip(ds model.DataStore, ff ffmpeg.FFmpeg) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := r.URL.Query()
		mediaFileID := q.Get("media_file_id")

		startMs, errStart := strconv.Atoi(q.Get("start_ms"))
		endMs, errEnd := strconv.Atoi(q.Get("end_ms"))
		if mediaFileID == "" || errStart != nil || errEnd != nil || startMs < 0 || endMs <= startMs {
			http.Error(w, "invalid clip parameters", http.StatusBadRequest)
			return
		}

		mf, err := ds.MediaFile(ctx).Get(mediaFileID)
		if errors.Is(err, model.ErrNotFound) {
			http.Error(w, "media file not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Error(ctx, "Error retrieving media file for music card clip", "id", mediaFileID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		paddedStart := startMs - clipPaddingMs
		if paddedStart < 0 {
			paddedStart = 0
		}
		paddedEnd := endMs + clipPaddingMs
		if trackDurationMs := int(mf.Duration * 1000); trackDurationMs > 0 && paddedEnd > trackDurationMs {
			paddedEnd = trackDurationMs
		}
		if paddedEnd <= paddedStart {
			http.Error(w, "invalid clip parameters", http.StatusBadRequest)
			return
		}

		rc, err := ff.Transcode(ctx, ffmpeg.TranscodeOptions{
			Format:        "mp3",
			FilePath:      mf.AbsolutePath(),
			BitRate:       128,
			StartOffsetMs: paddedStart,
			DurationMs:    paddedEnd - paddedStart,
		})
		if err != nil {
			log.Error(ctx, "Error extracting music card clip", "id", mediaFileID, err)
			http.Error(w, "error extracting clip", http.StatusInternalServerError)
			return
		}
		defer rc.Close()

		w.Header().Set("Content-Type", "audio/mpeg")
		if _, err := io.Copy(w, rc); err != nil {
			log.Error(ctx, "Error streaming music card clip", "id", mediaFileID, err)
		}
	}
}
