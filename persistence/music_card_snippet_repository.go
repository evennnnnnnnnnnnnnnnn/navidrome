package persistence

import (
	"errors"
	"time"

	"context"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/pocketbase/dbx"
)

// musicCardSnippetRepository has no user_id column of its own - a snippet's ownership is
// transitive through its parent music_card row, so every ownership check below joins/subqueries
// against music_card instead of using the generic addRestriction/updateOwned/deleteOwned helpers,
// which assume an unqualified user_id column on the repository's own table.
type musicCardSnippetRepository struct {
	sqlRepository
}

func NewMusicCardSnippetRepository(ctx context.Context, db dbx.Builder) model.MusicCardSnippetRepository {
	r := &musicCardSnippetRepository{}
	r.ctx = ctx
	r.db = db
	r.registerModel(&model.MusicCardSnippet{}, nil)
	return r
}

// ownerFilter returns the predicate restricting access to snippets whose parent card is owned by
// the logged-in user. It returns nil for admins and for headless/system contexts (invalid user),
// meaning "no ownership restriction" - mirroring sqlRepository.ownerFilter's contract, but scoped
// through card_id instead of an unqualified user_id column.
func (r *musicCardSnippetRepository) ownerFilter() Sqlizer {
	if usr := loggedUser(r.ctx); !usr.IsAdmin && usr.ID != invalidUserId {
		return Expr("card_id in (select id from music_card where user_id = ?)", usr.ID)
	}
	return nil
}

func (r *musicCardSnippetRepository) newRestSelect(options ...model.QueryOptions) SelectBuilder {
	sel := r.newSelect(options...)
	if owner := r.ownerFilter(); owner != nil {
		sel = sel.Where(owner)
	}
	return sel
}

func (r *musicCardSnippetRepository) CountAll(options ...model.QueryOptions) (int64, error) {
	return r.count(r.newRestSelect(), options...)
}

func (r *musicCardSnippetRepository) Count(options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(r.parseRestOptions(r.ctx, options...))
}

func (r *musicCardSnippetRepository) Get(id string) (*model.MusicCardSnippet, error) {
	sel := r.newRestSelect().Where(Eq{"id": id}).Columns("*")
	res := model.MusicCardSnippet{}
	err := r.queryOne(sel, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *musicCardSnippetRepository) GetAll(options ...model.QueryOptions) (model.MusicCardSnippets, error) {
	sel := r.newRestSelect(options...).Columns("*")
	res := model.MusicCardSnippets{}
	err := r.queryAll(sel, &res)
	return res, err
}

// cardOwnerID returns the user_id of the card identified by cardID, or model.ErrNotFound if it
// does not exist.
func (r *musicCardSnippetRepository) cardOwnerID(cardID string) (string, error) {
	sel := Select("user_id").From("music_card").Where(Eq{"id": cardID})
	var res struct{ UserID string }
	err := r.queryOne(sel, &res)
	if err != nil {
		return "", err
	}
	return res.UserID, nil
}

// checkCardOwnership verifies the caller may attach/see snippets on cardID: admins and headless
// contexts pass unconditionally (once the card is confirmed to exist), everyone else must own the
// card. Returns model.ErrNotFound if the card doesn't exist, rest.ErrPermissionDenied if it exists
// but belongs to someone else.
func (r *musicCardSnippetRepository) checkCardOwnership(cardID string) error {
	ownerID, err := r.cardOwnerID(cardID)
	if err != nil {
		return err
	}
	usr := loggedUser(r.ctx)
	if !usr.IsAdmin && usr.ID != invalidUserId && ownerID != usr.ID {
		return rest.ErrPermissionDenied
	}
	return nil
}

// Put creates a new snippet on the given card, verifying the caller owns (or is admin of) that
// card - the request payload's card_id is trusted only after this check, so a snippet can never be
// attached to another user's card by spoofing card_id.
func (r *musicCardSnippetRepository) Put(s *model.MusicCardSnippet) error {
	if err := r.checkCardOwnership(s.CardID); err != nil {
		return err
	}
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now
	s.ID = id.NewRandom()
	_, err := r.put(s.ID, s)
	return err
}

// classifyOwnedSnippetWriteMiss explains why an ownership-filtered write (Update/Delete) matched no
// row: rest.ErrPermissionDenied if the row exists but its card is owned by another user, otherwise
// rest.ErrNotFound.
func (r *musicCardSnippetRepository) classifyOwnedSnippetWriteMiss(id string) error {
	exists, err := r.exists(Eq{"id": id})
	if err != nil {
		return err
	}
	if exists {
		return rest.ErrPermissionDenied
	}
	return rest.ErrNotFound
}

// Delete performs an atomic, ownership-restricted delete: the card-ownership predicate is part of
// the DELETE's WHERE clause, so a snippet whose card is owned by another user simply does not
// match and is left untouched.
func (r *musicCardSnippetRepository) Delete(id string) error {
	where := And{Eq{"id": id}}
	if owner := r.ownerFilter(); owner != nil {
		where = append(where, owner)
	}
	count, err := r.executeSQL(Delete(r.tableName).Where(where))
	if err != nil {
		return err
	}
	if count == 0 {
		return r.classifyOwnedSnippetWriteMiss(id)
	}
	return nil
}

func (r *musicCardSnippetRepository) EntityName() string {
	return "music_card_snippet"
}

func (r *musicCardSnippetRepository) NewInstance() any {
	return &model.MusicCardSnippet{}
}

func (r *musicCardSnippetRepository) Read(id string) (any, error) {
	return r.Get(id)
}

func (r *musicCardSnippetRepository) ReadAll(options ...rest.QueryOptions) (any, error) {
	return r.GetAll(r.parseRestOptions(r.ctx, options...))
}

func (r *musicCardSnippetRepository) Save(entity any) (string, error) {
	s := entity.(*model.MusicCardSnippet)
	err := r.Put(s)
	if errors.Is(err, model.ErrNotFound) {
		return "", rest.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s.ID, nil
}

// Update is ownership-restricted through the snippet's existing card, and card_id/media_file_id
// are immutable identity fields - never written on update, so a snippet cannot be reassigned to a
// different card via a spoofed payload.
func (r *musicCardSnippetRepository) Update(id string, entity any, cols ...string) error {
	s := entity.(*model.MusicCardSnippet)
	s.ID = id
	s.UpdatedAt = time.Now()

	values, err := toSQLArgs(s)
	if err != nil {
		return err
	}
	if len(cols) > 0 {
		cols = append(cols, "updated_at")
	}
	updateValues := filterUpdateValues(values, id, cols...)
	delete(updateValues, "card_id")
	delete(updateValues, "media_file_id")

	where := And{Eq{"id": id}}
	if owner := r.ownerFilter(); owner != nil {
		where = append(where, owner)
	}
	count, err := r.executeSQL(Update(r.tableName).Where(where).SetMap(updateValues))
	if err != nil {
		return err
	}
	if count == 0 {
		return r.classifyOwnedSnippetWriteMiss(id)
	}
	return nil
}

var _ model.MusicCardSnippetRepository = (*musicCardSnippetRepository)(nil)
var _ rest.Repository = (*musicCardSnippetRepository)(nil)
var _ rest.Persistable = (*musicCardSnippetRepository)(nil)
