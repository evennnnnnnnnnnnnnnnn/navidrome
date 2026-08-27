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

type furiganaBindingRepository struct {
	sqlRepository
}

func NewFuriganaBindingRepository(ctx context.Context, db dbx.Builder) model.FuriganaBindingRepository {
	r := &furiganaBindingRepository{}
	r.ctx = ctx
	r.db = db
	r.registerModel(&model.FuriganaBinding{}, nil)
	return r
}

// newRestSelect returns a select scoped to the current user: ownerFilter() restricts non-admin,
// non-headless callers to their own rows and is a no-op (nil) for admins.
func (r *furiganaBindingRepository) newRestSelect(options ...model.QueryOptions) SelectBuilder {
	return r.newSelect(options...).Where(r.addRestriction())
}

func (r *furiganaBindingRepository) CountAll(options ...model.QueryOptions) (int64, error) {
	return r.count(r.newRestSelect(), options...)
}

func (r *furiganaBindingRepository) Count(options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(r.parseRestOptions(r.ctx, options...))
}

func (r *furiganaBindingRepository) Get(id string) (*model.FuriganaBinding, error) {
	sel := r.newRestSelect().Where(Eq{"id": id}).Columns("*")
	res := model.FuriganaBinding{}
	err := r.queryOne(sel, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *furiganaBindingRepository) GetAll(options ...model.QueryOptions) (model.FuriganaBindings, error) {
	sel := r.newRestSelect(options...).Columns("*")
	res := model.FuriganaBindings{}
	err := r.queryAll(sel, &res)
	return res, err
}

// naturalKey identifies a binding by (user, song, line, char offset) - the Museeks
// addBinding/updateBinding match key - regardless of its synthetic id.
func (r *furiganaBindingRepository) naturalKey(fb *model.FuriganaBinding) Sqlizer {
	return And{
		Eq{"user_id": fb.UserID},
		Eq{"media_file_id": fb.MediaFileID},
		Eq{"line_index": fb.LineIndex},
		Eq{"char_offset": fb.CharOffset},
	}
}

func (r *furiganaBindingRepository) findByNaturalKey(fb *model.FuriganaBinding) (string, error) {
	sel := Select("id").From(r.tableName).Where(r.naturalKey(fb))
	var res struct{ ID string }
	err := r.queryOne(sel, &res)
	if err != nil {
		return "", err
	}
	return res.ID, nil
}

// Put creates a new binding or replaces the existing one at the same natural key, always
// forcing the owning user_id from the authenticated context - the request payload's user_id,
// if any, is never trusted.
func (r *furiganaBindingRepository) Put(fb *model.FuriganaBinding) error {
	fb.UserID = loggedUser(r.ctx).ID
	fb.UpdatedAt = time.Now()

	existingID, err := r.findByNaturalKey(fb)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if existingID != "" {
		fb.ID = existingID
	} else {
		fb.CreatedAt = fb.UpdatedAt
		fb.ID = id.NewRandom()
	}
	_, err = r.put(fb.ID, fb)
	return err
}

func (r *furiganaBindingRepository) Delete(id string) error {
	return r.deleteOwned(id)
}

func (r *furiganaBindingRepository) EntityName() string {
	return "furigana_binding"
}

func (r *furiganaBindingRepository) NewInstance() any {
	return &model.FuriganaBinding{}
}

func (r *furiganaBindingRepository) Read(id string) (any, error) {
	return r.Get(id)
}

func (r *furiganaBindingRepository) ReadAll(options ...rest.QueryOptions) (any, error) {
	return r.GetAll(r.parseRestOptions(r.ctx, options...))
}

// Save always routes through Put, so a POST that targets an existing natural key upserts in
// place instead of tripping the unique constraint.
func (r *furiganaBindingRepository) Save(entity any) (string, error) {
	fb := entity.(*model.FuriganaBinding)
	err := r.Put(fb)
	if errors.Is(err, model.ErrNotFound) {
		return "", rest.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return fb.ID, nil
}

// Update is ownership-restricted: updateOwned matches the row only when it is owned by the
// caller (or the caller is admin) and never writes user_id, so ownership cannot be reassigned
// via a spoofed payload.
func (r *furiganaBindingRepository) Update(id string, entity any, cols ...string) error {
	fb := entity.(*model.FuriganaBinding)
	fb.ID = id
	fb.UpdatedAt = time.Now()
	if len(cols) > 0 {
		cols = append(cols, "updated_at")
	}
	return r.updateOwned(id, fb, cols...)
}

var _ model.FuriganaBindingRepository = (*furiganaBindingRepository)(nil)
var _ rest.Repository = (*furiganaBindingRepository)(nil)
var _ rest.Persistable = (*furiganaBindingRepository)(nil)
