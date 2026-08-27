package persistence

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type lyricsOverrideRepository struct {
	sqlRepository
}

func NewLyricsOverrideRepository(ctx context.Context, db dbx.Builder) model.LyricsOverrideRepository {
	r := &lyricsOverrideRepository{}
	r.ctx = ctx
	r.db = db
	r.registerModel(&model.LyricsOverride{}, nil)
	return r
}

func (r *lyricsOverrideRepository) isPermitted() bool {
	user := loggedUser(r.ctx)
	return user.IsAdmin
}

func (r *lyricsOverrideRepository) Get(mediaFileID string) (*model.LyricsOverride, error) {
	sel := r.newSelect().Where(Eq{"media_file_id": mediaFileID}).Columns("*")
	res := model.LyricsOverride{}
	err := r.queryOne(sel, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Put creates or replaces the override for a media file, resolving the acting
// user from the request context (never from caller input) for the updated_by
// audit column.
func (r *lyricsOverrideRepository) Put(mediaFileID string, lyrics model.LyricList) error {
	if !r.isPermitted() {
		return rest.ErrPermissionDenied
	}
	data, err := json.Marshal(lyrics)
	if err != nil {
		return err
	}
	now := time.Now()
	values := map[string]any{
		"lyrics":     string(data),
		"updated_by": loggedUser(r.ctx).ID,
		"updated_at": now,
	}

	upd := Update(r.tableName).Where(Eq{"media_file_id": mediaFileID}).SetMap(values)
	count, err := r.executeSQL(upd)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	values["media_file_id"] = mediaFileID
	values["created_at"] = now
	ins := Insert(r.tableName).SetMap(values)
	_, err = r.executeSQL(ins)
	return err
}

func (r *lyricsOverrideRepository) Delete(mediaFileID string) error {
	if !r.isPermitted() {
		return rest.ErrPermissionDenied
	}
	return r.delete(Eq{"media_file_id": mediaFileID})
}

var _ model.LyricsOverrideRepository = (*lyricsOverrideRepository)(nil)
