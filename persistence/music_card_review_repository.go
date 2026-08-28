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

// musicCardReviewRepository has no user_id column of its own - a review row's ownership is
// transitive through its parent music_card row, mirroring musicCardSnippetRepository. The
// repository is read-only through the generic REST layer (no rest.Persistable); the only write
// path is Put, called by the grade endpoint after applying the scheduler transition.
type musicCardReviewRepository struct {
	sqlRepository
}

// dueBeforeFilter lets the REST list endpoint restrict to cards due at or before a moment
// (`?due_before=<RFC3339>`), which is the "give me today's queue" query. Both sides go through
// sqlite's datetime() so stored values and the query value compare as normalized UTC - the driver
// stores timestamps with their own utc-offset suffix, and a naive string comparison against a
// value in a different offset would order them lexically, not chronologically. An unparseable
// value matches nothing rather than silently returning the full list.
func dueBeforeFilter(_ string, value any) Sqlizer {
	s, ok := value.(string)
	if !ok {
		return Expr("1 = 0")
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return Expr("1 = 0")
	}
	return Expr("datetime(due_at) <= datetime(?)", s)
}

func NewMusicCardReviewRepository(ctx context.Context, db dbx.Builder) model.MusicCardReviewRepository {
	r := &musicCardReviewRepository{}
	r.ctx = ctx
	r.db = db
	r.registerModel(&model.MusicCardReview{}, map[string]filterFunc{
		"due_before": dueBeforeFilter,
	})
	return r
}

// ownerFilter returns the predicate restricting access to review rows whose parent card is owned
// by the logged-in user. It returns nil for admins and for headless/system contexts (invalid
// user), meaning "no ownership restriction" - the same contract as
// musicCardSnippetRepository.ownerFilter.
func (r *musicCardReviewRepository) ownerFilter() Sqlizer {
	if usr := loggedUser(r.ctx); !usr.IsAdmin && usr.ID != invalidUserId {
		return Expr("card_id in (select id from music_card where user_id = ?)", usr.ID)
	}
	return nil
}

func (r *musicCardReviewRepository) newRestSelect(options ...model.QueryOptions) SelectBuilder {
	sel := r.newSelect(options...)
	if owner := r.ownerFilter(); owner != nil {
		sel = sel.Where(owner)
	}
	return sel
}

func (r *musicCardReviewRepository) CountAll(options ...model.QueryOptions) (int64, error) {
	return r.count(r.newRestSelect(), options...)
}

func (r *musicCardReviewRepository) Count(options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(r.parseRestOptions(r.ctx, options...))
}

func (r *musicCardReviewRepository) Get(id string) (*model.MusicCardReview, error) {
	sel := r.newRestSelect().Where(Eq{"id": id}).Columns("*")
	res := model.MusicCardReview{}
	err := r.queryOne(sel, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *musicCardReviewRepository) GetAll(options ...model.QueryOptions) (model.MusicCardReviews, error) {
	sel := r.newRestSelect(options...).Columns("*")
	res := model.MusicCardReviews{}
	err := r.queryAll(sel, &res)
	return res, err
}

func (r *musicCardReviewRepository) GetByCardID(cardID string) (*model.MusicCardReview, error) {
	sel := r.newRestSelect().Where(Eq{"card_id": cardID}).Columns("*")
	res := model.MusicCardReview{}
	err := r.queryOne(sel, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// cardOwnerID returns the user_id of the card identified by cardID, or model.ErrNotFound if it
// does not exist.
func (r *musicCardReviewRepository) cardOwnerID(cardID string) (string, error) {
	sel := Select("user_id").From("music_card").Where(Eq{"id": cardID})
	var res struct{ UserID string }
	err := r.queryOne(sel, &res)
	if err != nil {
		return "", err
	}
	return res.UserID, nil
}

// checkCardOwnership verifies the caller may hold review state on cardID: admins and headless
// contexts pass unconditionally (once the card is confirmed to exist), everyone else must own the
// card. Returns model.ErrNotFound if the card doesn't exist, rest.ErrPermissionDenied if it
// exists but belongs to someone else.
func (r *musicCardReviewRepository) checkCardOwnership(cardID string) error {
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

// Put upserts the review row at its card_id natural key, verifying the caller owns (or is admin
// of) that card first - the payload's card_id is trusted only after this check, so review state
// can never be attached to another user's card.
func (r *musicCardReviewRepository) Put(rev *model.MusicCardReview) error {
	if err := r.checkCardOwnership(rev.CardID); err != nil {
		return err
	}
	rev.UpdatedAt = time.Now()

	sel := Select("id").From(r.tableName).Where(Eq{"card_id": rev.CardID})
	var existing struct{ ID string }
	err := r.queryOne(sel, &existing)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if existing.ID != "" {
		rev.ID = existing.ID
	} else {
		rev.CreatedAt = rev.UpdatedAt
		rev.ID = id.NewRandom()
	}
	_, err = r.put(rev.ID, rev)
	return err
}

func (r *musicCardReviewRepository) EntityName() string {
	return "music_card_review"
}

func (r *musicCardReviewRepository) NewInstance() any {
	return &model.MusicCardReview{}
}

func (r *musicCardReviewRepository) Read(id string) (any, error) {
	return r.Get(id)
}

func (r *musicCardReviewRepository) ReadAll(options ...rest.QueryOptions) (any, error) {
	return r.GetAll(r.parseRestOptions(r.ctx, options...))
}

var _ model.MusicCardReviewRepository = (*musicCardReviewRepository)(nil)
var _ rest.Repository = (*musicCardReviewRepository)(nil)
