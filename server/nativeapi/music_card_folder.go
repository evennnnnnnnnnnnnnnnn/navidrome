package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

// addMusicCardFolderRoute registers the folder resource: the generic api.R registration with two
// wrappers, because the generic controller answers "not yours" with 403 and knows nothing about the
// undeletable default folder.
func (api *Router) addMusicCardFolderRoute(r chi.Router) {
	constructor := func(ctx context.Context) rest.Repository {
		return api.ds.Resource(ctx, model.MusicCardFolder{})
	}
	r.Route("/musiccardfolder", func(r chi.Router) {
		r.Get("/", rest.GetAll(constructor))
		r.Post("/", rest.Post(constructor))
		r.Route("/{id}", func(r chi.Router) {
			r.Use(server.URLParamsMiddleware)
			r.Get("/", rest.Get(constructor))
			r.Put("/", putMusicCardFolder(constructor))
			r.Delete("/", deleteMusicCardFolder(api.ds))
		})
	})
}

// putMusicCardFolder is rest.Put over a repository that reports a folder the caller does not own as
// missing, so a private folder's existence never leaks through a 403. Validation errors still come
// back as 400.
func putMusicCardFolder(constructor rest.RepositoryConstructor) http.HandlerFunc {
	return rest.Put(func(ctx context.Context) rest.Repository {
		repo := constructor(ctx)
		return &hiddenDeniedFolderRepository{Repository: repo, Persistable: repo.(rest.Persistable)}
	})
}

type hiddenDeniedFolderRepository struct {
	rest.Repository
	rest.Persistable
}

func (r *hiddenDeniedFolderRepository) Update(id string, entity any, cols ...string) error {
	if err := r.Persistable.Update(id, entity, cols...); err != nil {
		if errors.Is(err, rest.ErrPermissionDenied) {
			return rest.ErrNotFound
		}
		return err
	}
	return nil
}

func deleteMusicCardFolder(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		err := ds.MusicCardFolder(ctx).Delete(chi.URLParam(r, "id"))
		var verr *rest.ValidationError
		switch {
		case err == nil:
			rest.RespondWithJSON(w, http.StatusOK, &map[string]string{})
		case errors.As(err, &verr):
			rest.RespondWithJSON(w, http.StatusBadRequest, verr)
		default:
			respondMusicCardFolderError(ctx, w, err)
		}
	}
}

// addMusicCardFolderCardsRoute registers the membership endpoints for Music Card folders. The
// generic /musiccardfolder resource covers the folder itself; the cards of a folder need their own
// routes because reading them is the one deliberate cross-user read in the card system (a public
// folder is readable by every user) and writing them is owner-only.
func (api *Router) addMusicCardFolderCardsRoute(r chi.Router) {
	r.Route("/musiccardfolder/{folderId}/cards", func(r chi.Router) {
		r.Get("/", getMusicCardFolderCards(api.ds))
		r.Post("/", addMusicCardFolderCards(api.ds))
		r.Delete("/{cardId}", removeMusicCardFolderCard(api.ds))
	})
}

type musicCardFolderCardsRequest struct {
	CardIDs []string `json:"card_ids"`
}

func getMusicCardFolderCards(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		cards, err := ds.MusicCardFolder(ctx).Cards(chi.URLParam(r, "folderId"))
		if err != nil {
			respondMusicCardFolderError(ctx, w, err)
			return
		}
		writeMusicCardFolderJSON(ctx, w, cards)
	}
}

func addMusicCardFolderCards(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var payload musicCardFolderCardsRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err := ds.MusicCardFolder(ctx).AddCards(chi.URLParam(r, "folderId"), payload.CardIDs)
		if err != nil {
			respondMusicCardFolderError(ctx, w, err)
			return
		}
		writeMusicCardFolderJSON(ctx, w, struct{}{})
	}
}

func removeMusicCardFolderCard(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		err := ds.MusicCardFolder(ctx).RemoveCard(chi.URLParam(r, "folderId"), chi.URLParam(r, "cardId"))
		if err != nil {
			respondMusicCardFolderError(ctx, w, err)
			return
		}
		writeMusicCardFolderJSON(ctx, w, struct{}{})
	}
}

// respondMusicCardFolderError maps a missing folder and a folder the caller may not touch to the
// same 404, so the endpoint never leaks the existence of another user's private folder.
func respondMusicCardFolderError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound), errors.Is(err, rest.ErrNotFound), errors.Is(err, rest.ErrPermissionDenied):
		http.Error(w, "music card folder not found", http.StatusNotFound)
	default:
		log.Error(ctx, "Error handling music card folder cards request", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeMusicCardFolderJSON(ctx context.Context, w http.ResponseWriter, payload any) {
	resp, err := json.Marshal(payload)
	if err != nil {
		log.Error(ctx, "Error marshaling music card folder response", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(resp) //nolint:gosec
}
