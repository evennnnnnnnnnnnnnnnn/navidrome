package persistence

import (
	"context"
	"errors"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/pocketbase/dbx"
)

type musicCardRepository struct {
	sqlRepository
}

func NewMusicCardRepository(ctx context.Context, db dbx.Builder) model.MusicCardRepository {
	r := &musicCardRepository{}
	r.ctx = ctx
	r.db = db
	// Cards are private study data, not an administrable resource, so ownership scoping applies to
	// admins too - this is what makes addRestriction/updateOwned/deleteOwned scope every logged-in
	// caller below.
	r.strictOwnership = true
	r.registerModel(&model.MusicCard{}, nil)
	return r
}

// newRestSelect returns a select scoped to the current user: ownerFilter() restricts every
// logged-in caller, admins included, to their own rows, and is a no-op (nil) only for headless
// contexts.
func (r *musicCardRepository) newRestSelect(options ...model.QueryOptions) SelectBuilder {
	return r.newSelect(options...).Where(r.addRestriction())
}

func (r *musicCardRepository) CountAll(options ...model.QueryOptions) (int64, error) {
	return r.count(r.newRestSelect(), options...)
}

func (r *musicCardRepository) Count(options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(r.parseRestOptions(r.ctx, options...))
}

func (r *musicCardRepository) Get(id string) (*model.MusicCard, error) {
	sel := r.newRestSelect().Where(Eq{"id": id}).Columns("*")
	res := model.MusicCard{}
	err := r.queryOne(sel, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *musicCardRepository) GetAll(options ...model.QueryOptions) (model.MusicCards, error) {
	sel := r.newRestSelect(options...).Columns("*")
	res := model.MusicCards{}
	err := r.queryAll(sel, &res)
	return res, err
}

// naturalKey identifies a card by (user, kanji_text) - saving the same kanji run from a new
// context must append a snippet to this card rather than creating a duplicate.
func (r *musicCardRepository) naturalKey(mc *model.MusicCard) Sqlizer {
	return And{
		Eq{"user_id": mc.UserID},
		Eq{"kanji_text": mc.KanjiText},
	}
}

func (r *musicCardRepository) findByNaturalKey(mc *model.MusicCard) (string, error) {
	sel := Select("id").From(r.tableName).Where(r.naturalKey(mc))
	var res struct{ ID string }
	err := r.queryOne(sel, &res)
	if err != nil {
		return "", err
	}
	return res.ID, nil
}

// Put creates a new card or returns the existing one at the same natural key, always forcing the
// owning user_id from the authenticated context - the request payload's user_id, if any, is never
// trusted.
func (r *musicCardRepository) Put(mc *model.MusicCard) error {
	mc.UserID = loggedUser(r.ctx).ID
	mc.UpdatedAt = time.Now()

	existingID, err := r.findByNaturalKey(mc)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if existingID != "" {
		mc.ID = existingID
	} else {
		mc.CreatedAt = mc.UpdatedAt
		mc.ID = id.NewRandom()
	}
	_, err = r.put(mc.ID, mc)
	return err
}

func (r *musicCardRepository) Delete(id string) error {
	return r.deleteOwned(id)
}

func (r *musicCardRepository) EntityName() string {
	return "music_card"
}

func (r *musicCardRepository) NewInstance() any {
	return &model.MusicCard{}
}

func (r *musicCardRepository) Read(id string) (any, error) {
	return r.Get(id)
}

func (r *musicCardRepository) ReadAll(options ...rest.QueryOptions) (any, error) {
	return r.GetAll(r.parseRestOptions(r.ctx, options...))
}

// Save always routes through Put, so a POST that targets an existing natural key upserts in place
// instead of tripping the unique constraint.
func (r *musicCardRepository) Save(entity any) (string, error) {
	mc := entity.(*model.MusicCard)
	err := r.Put(mc)
	if errors.Is(err, model.ErrNotFound) {
		return "", rest.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return mc.ID, nil
}

// Update is ownership-restricted: updateOwned matches the row only when it is owned by the caller
// and never writes user_id, so ownership cannot be reassigned via a spoofed payload.
func (r *musicCardRepository) Update(id string, entity any, cols ...string) error {
	mc := entity.(*model.MusicCard)
	mc.ID = id
	mc.UpdatedAt = time.Now()
	if len(cols) > 0 {
		cols = append(cols, "updated_at")
	}
	return r.updateOwned(id, mc, cols...)
}

var _ model.MusicCardRepository = (*musicCardRepository)(nil)
var _ rest.Repository = (*musicCardRepository)(nil)
var _ rest.Persistable = (*musicCardRepository)(nil)
