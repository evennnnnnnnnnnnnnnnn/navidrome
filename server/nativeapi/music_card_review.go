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

		// The read-transition-write sequence runs inside an immediate transaction so concurrent
		// grades for the same card serialize instead of overwriting each other's state (and two
		// racing first grades cannot both try to create the unique card_id row). The repository
		// scopes reads to the caller's cards, so a card owned by someone else is indistinguishable
		// from a missing one (404, no information leak).
		now := time.Now()
		var rev *model.MusicCardReview
		txErr := ds.WithTxImmediate(func(tx model.DataStore) error {
			repo := tx.MusicCardReview(ctx)
			var err error
			rev, err = repo.GetByCardID(payload.CardID)
			if errors.Is(err, model.ErrNotFound) {
				if _, err := tx.MusicCard(ctx).Get(payload.CardID); err != nil {
					return err
				}
				rev = model.NewMusicCardReview(payload.CardID, now)
			} else if err != nil {
				return err
			}
			if err := rev.ApplyGrade(grade, now); err != nil {
				return err
			}
			return repo.Put(rev)
		})
		if txErr != nil {
			switch {
			case errors.Is(txErr, model.ErrNotFound), errors.Is(txErr, rest.ErrPermissionDenied):
				http.Error(w, "music card not found", http.StatusNotFound)
			case errors.Is(txErr, model.ErrValidation):
				http.Error(w, txErr.Error(), http.StatusBadRequest)
			default:
				log.Error(ctx, "Error grading music card review", "cardId", payload.CardID, txErr)
				http.Error(w, txErr.Error(), http.StatusInternalServerError)
			}
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
