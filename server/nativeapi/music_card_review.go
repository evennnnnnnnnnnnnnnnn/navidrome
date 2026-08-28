package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// addMusicCardReviewGradeRoute registers the grade endpoint for Music Card SRS review state. The
// SM-2-style transition runs server-side (model.MusicCardReview.ApplyGrade) so every client sees
// the same schedule; the generic /musiccardreview REST resource stays read-only and this POST is
// the only write path.
func (api *Router) addMusicCardReviewGradeRoute(r chi.Router) {
	r.Post("/musiccardreview/grade", postMusicCardReviewGrade(api.ds))
}

type musicCardReviewGradeRequest struct {
	CardID string `json:"card_id"`
	Grade  string `json:"grade"`
}

func postMusicCardReviewGrade(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var payload musicCardReviewGradeRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.CardID == "" {
			http.Error(w, "card_id is required", http.StatusBadRequest)
			return
		}
		grade, err := model.ParseReviewGrade(payload.Grade)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// The repository scopes reads to the caller's cards, so a card owned by someone else is
		// indistinguishable from a missing one (404, no information leak).
		repo := ds.MusicCardReview(ctx)
		now := time.Now()
		rev, err := repo.GetByCardID(payload.CardID)
		if errors.Is(err, model.ErrNotFound) {
			if _, err := ds.MusicCard(ctx).Get(payload.CardID); errors.Is(err, model.ErrNotFound) {
				http.Error(w, "music card not found", http.StatusNotFound)
				return
			} else if err != nil {
				log.Error(ctx, "Error retrieving music card for review grade", "cardId", payload.CardID, err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rev = model.NewMusicCardReview(payload.CardID, now)
		} else if err != nil {
			log.Error(ctx, "Error retrieving music card review state", "cardId", payload.CardID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := rev.ApplyGrade(grade, now); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := repo.Put(rev); err != nil {
			if errors.Is(err, rest.ErrPermissionDenied) {
				http.Error(w, "music card not found", http.StatusNotFound)
				return
			}
			log.Error(ctx, "Error saving music card review state", "cardId", payload.CardID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(rev)
		if err != nil {
			log.Error(ctx, "Error marshaling music card review state", "cardId", payload.CardID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp) //nolint:gosec
	}
}
