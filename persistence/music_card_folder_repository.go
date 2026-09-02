package persistence

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/pocketbase/dbx"
)

const defaultMusicCardFolderName = "Default"

// user_id and is_default are server-owned, so they are never in the writable set.
var mutableMusicCardFolderCols = []string{"name", "public", "review_enabled"}

// Unlike playlists, the visibility filter guards reads only: writes go through the strictOwnership
// helpers, so a public folder is readable by everyone but modifiable only by its owner.
type musicCardFolderRepository struct {
	sqlRepository
}

func NewMusicCardFolderRepository(ctx context.Context, db dbx.Builder) model.MusicCardFolderRepository {
	r := &musicCardFolderRepository{}
	r.ctx = ctx
	r.db = db
	r.strictOwnership = true
	r.registerModel(&model.MusicCardFolder{}, nil)
	return r
}

// visibleFilter is nil for headless contexts, meaning "no restriction".
func (r *musicCardFolderRepository) visibleFilter() Sqlizer {
	if usr := loggedUser(r.ctx); usr.ID != invalidUserId {
		return Or{Eq{"music_card_folder.user_id": usr.ID}, Eq{"music_card_folder.public": true}}
	}
	return nil
}

func (r *musicCardFolderRepository) newRestSelect(options ...model.QueryOptions) SelectBuilder {
	sel := r.newSelect(options...).
		Join("user on user.id = music_card_folder.user_id").
		Columns("music_card_folder.*", "user.user_name as owner_name")
	if visible := r.visibleFilter(); visible != nil {
		sel = sel.Where(visible)
	}
	return sel
}

func (r *musicCardFolderRepository) CountAll(options ...model.QueryOptions) (int64, error) {
	sel := Select()
	if visible := r.visibleFilter(); visible != nil {
		sel = sel.Where(visible)
	}
	return r.count(sel, options...)
}

func (r *musicCardFolderRepository) Count(options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(r.parseRestOptions(r.ctx, options...))
}

func (r *musicCardFolderRepository) Get(id string) (*model.MusicCardFolder, error) {
	sel := r.newRestSelect().Where(Eq{"music_card_folder.id": id})
	res := model.MusicCardFolders{model.MusicCardFolder{}}
	if err := r.queryOne(sel, &res[0]); err != nil {
		return nil, err
	}
	if err := r.loadCardIDs(res); err != nil {
		return nil, err
	}
	return &res[0], nil
}

// GetAll ensures the default folder exists, so the deck appears on first use for a user created
// after the backfill migration.
func (r *musicCardFolderRepository) GetAll(options ...model.QueryOptions) (model.MusicCardFolders, error) {
	if _, err := r.EnsureDefault(); err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	sel := r.newRestSelect(options...)
	if len(options) == 0 || options[0].Sort == "" {
		sel = sel.OrderBy("music_card_folder.is_default desc", "music_card_folder.name asc")
	}
	res := model.MusicCardFolders{}
	if err := r.queryAll(sel, &res); err != nil {
		return nil, err
	}
	if err := r.loadCardIDs(res); err != nil {
		return nil, err
	}
	return res, nil
}

// loadCardIDs fills CardIDs/CardCount in one query that re-applies the visibility rule, so a folder
// turned private between the two statements cannot leak its membership.
func (r *musicCardFolderRepository) loadCardIDs(folders model.MusicCardFolders) error {
	if len(folders) == 0 {
		return nil
	}
	ids := make([]string, len(folders))
	for i := range folders {
		ids[i] = folders[i].ID
		folders[i].CardIDs = []string{}
	}
	var rows []struct {
		FolderID string
		CardID   string
	}
	sel := Select("music_card_folder_card.folder_id as folder_id", "music_card_folder_card.card_id as card_id").
		From("music_card_folder_card").
		Join("music_card_folder on music_card_folder.id = music_card_folder_card.folder_id").
		Where(r.addVisibility(Eq{"music_card_folder_card.folder_id": ids})).
		OrderBy("music_card_folder_card.created_at", "music_card_folder_card.card_id")
	if err := r.queryAll(sel, &rows); err != nil {
		return err
	}
	byID := map[string]*model.MusicCardFolder{}
	for i := range folders {
		byID[folders[i].ID] = &folders[i]
	}
	for _, row := range rows {
		if f, ok := byID[row.FolderID]; ok {
			f.CardIDs = append(f.CardIDs, row.CardID)
			f.CardCount++
		}
	}
	return nil
}

// Put forces user_id from the authenticated context and ignores any is_default in the payload.
func (r *musicCardFolderRepository) Put(f *model.MusicCardFolder) error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		return folderNameRequiredError()
	}
	// Materialise the default deck first: its reserved name would otherwise be squattable by an
	// ordinary folder, locking lazy creation out for good.
	if _, err := r.EnsureDefault(); err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	f.UserID = loggedUser(r.ctx).ID
	f.IsDefault = false
	f.CreatedAt = time.Now()
	f.UpdatedAt = f.CreatedAt
	f.ID = id.NewRandom()
	_, err := r.put(f.ID, f)
	return folderNameConflict(err)
}

// Delete refuses the default deck, which every user must keep.
func (r *musicCardFolderRepository) Delete(id string) error {
	sel := Select("is_default").From(r.tableName).Where(r.addRestriction(Eq{"id": id}))
	var res struct{ IsDefault bool }
	if err := r.queryOne(sel, &res); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return r.classifyOwnedWriteMiss(id)
		}
		return err
	}
	if res.IsDefault {
		return &rest.ValidationError{Errors: map[string]string{"is_default": "the default folder cannot be deleted"}}
	}
	return r.deleteOwned(id)
}

// EnsureDefault returns the caller's default folder, creating it on first use. The partial unique
// index on (user_id) where is_default is what keeps it single.
func (r *musicCardFolderRepository) EnsureDefault() (*model.MusicCardFolder, error) {
	usr := loggedUser(r.ctx)
	if usr.ID == invalidUserId {
		return nil, model.ErrNotFound
	}
	f, err := r.findDefault(usr.ID)
	if err == nil || !errors.Is(err, model.ErrNotFound) {
		return f, err
	}
	return r.insertDefault(usr.ID)
}

func (r *musicCardFolderRepository) findDefault(userID string) (*model.MusicCardFolder, error) {
	sel := Select("*").From(r.tableName).Where(And{Eq{"user_id": userID}, Eq{"is_default": true}})
	res := model.MusicCardFolder{}
	if err := r.queryOne(sel, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// insertDefault creates the default folder, reselecting instead of failing when a concurrent first
// call already won the partial unique index.
func (r *musicCardFolderRepository) insertDefault(userID string) (*model.MusicCardFolder, error) {
	now := time.Now()
	f := model.MusicCardFolder{
		ID:            id.NewRandom(),
		UserID:        userID,
		Name:          defaultMusicCardFolderName,
		ReviewEnabled: true,
		IsDefault:     true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := r.put(f.ID, &f); err != nil {
		if winner, selErr := r.findDefault(userID); selErr == nil {
			return winner, nil
		}
		return nil, err
	}
	return &f, nil
}

func (r *musicCardFolderRepository) AddCardToDefault(cardID string) error {
	if loggedUser(r.ctx).ID == invalidUserId {
		return nil // headless contexts own no deck
	}
	f, err := r.EnsureDefault()
	if err != nil {
		return err
	}
	return r.insertMembership(f.ID, []string{cardID})
}

// AddCards is all-or-nothing, so a folder can never be used to smuggle another user's card into a
// public deck.
func (r *musicCardFolderRepository) AddCards(folderID string, cardIDs []string) error {
	ids := slices.Compact(slices.Sorted(slices.Values(cardIDs)))
	// Authorization, validation and the insert share one snapshot: otherwise a folder or card could
	// change owner between the check and the write.
	return musicCardTx(r.db, func(tx dbx.Builder) error {
		txr := r.withDB(tx)
		if err := txr.checkOwnedFolder(folderID); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if usr := loggedUser(r.ctx); usr.ID != invalidUserId {
			var res struct{ Count int64 }
			sel := Select("count(*) as count").From("music_card").Where(And{Eq{"id": ids}, Eq{"user_id": usr.ID}})
			if err := txr.queryOne(sel, &res); err != nil {
				return err
			}
			if int(res.Count) != len(ids) {
				return rest.ErrPermissionDenied
			}
		}
		return txr.insertMembership(folderID, ids)
	})
}

func (r *musicCardFolderRepository) RemoveCard(folderID, cardID string) error {
	if err := r.checkOwnedFolder(folderID); err != nil {
		return err
	}
	_, err := r.executeSQL(Delete("music_card_folder_card").
		Where(And{Eq{"folder_id": folderID}, Eq{"card_id": cardID}}))
	return err
}

// Cards is read under the folder's own visibility rule: a folder the caller cannot see is
// indistinguishable from a missing one.
func (r *musicCardFolderRepository) Cards(folderID string) (model.MusicCardsWithSnippets, error) {
	// The preliminary check only distinguishes an empty visible folder from an invisible one.
	visible, err := r.exists(r.addVisibility(Eq{"music_card_folder.id": folderID}))
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, model.ErrNotFound
	}

	// The visibility rule is re-applied in the statement that returns the cards, so revoking it
	// between the two queries cannot hand out a private folder's contents.
	cards := model.MusicCards{}
	sel := Select("music_card.*").From("music_card").
		Join("music_card_folder_card on music_card_folder_card.card_id = music_card.id").
		Join("music_card_folder on music_card_folder.id = music_card_folder_card.folder_id").
		Where(r.addVisibility(Eq{"music_card_folder_card.folder_id": folderID})).
		OrderBy("music_card_folder_card.created_at desc", "music_card.id")
	if err := r.queryAll(sel, &cards); err != nil {
		return nil, err
	}

	res := make(model.MusicCardsWithSnippets, len(cards))
	byCard := map[string]*model.MusicCardWithSnippets{}
	ids := make([]string, len(cards))
	for i, c := range cards {
		res[i] = model.MusicCardWithSnippets{MusicCard: c, Snippets: model.MusicCardSnippets{}}
		byCard[c.ID] = &res[i]
		ids[i] = c.ID
	}
	if len(ids) == 0 {
		return res, nil
	}

	snippets := model.MusicCardSnippets{}
	snipSel := Select("*").From("music_card_snippet").Where(Eq{"card_id": ids}).OrderBy("created_at", "id")
	if err := r.queryAll(snipSel, &snippets); err != nil {
		return nil, err
	}
	for _, s := range snippets {
		if c, ok := byCard[s.CardID]; ok {
			c.Snippets = append(c.Snippets, s)
		}
	}
	return res, nil
}

// addVisibility combines a caller predicate with the read visibility rule (own or public).
func (r *musicCardFolderRepository) addVisibility(cond Sqlizer) Sqlizer {
	s := And{cond}
	if visible := r.visibleFilter(); visible != nil {
		s = append(s, visible)
	}
	return s
}

// checkOwnedFolder maps the miss the same way updateOwned/deleteOwned do.
func (r *musicCardFolderRepository) checkOwnedFolder(folderID string) error {
	owned, err := r.exists(r.addRestriction(Eq{"id": folderID}))
	if err != nil {
		return err
	}
	if !owned {
		return r.classifyOwnedWriteMiss(folderID)
	}
	return nil
}

func (r *musicCardFolderRepository) insertMembership(folderID string, cardIDs []string) error {
	ins := Insert("music_card_folder_card").Options("OR IGNORE").
		Columns("folder_id", "card_id", "created_at")
	now := time.Now()
	for _, cardID := range cardIDs {
		ins = ins.Values(folderID, cardID, now)
	}
	_, err := r.executeSQL(ins)
	return err
}

func folderNameRequiredError() error {
	return &rest.ValidationError{Errors: map[string]string{"name": "ra.validation.required"}}
}

// folderNameConflict turns the (user_id, name) unique failure into the validation error the UI knows,
// instead of the raw SQLite message the generic REST layer would report as a 500. SQLite only names
// the offending columns in the message text - the same detection core/library.go uses.
func folderNameConflict(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") &&
		strings.Contains(err.Error(), "music_card_folder.name") {
		return &rest.ValidationError{Errors: map[string]string{"name": "ra.validation.unique"}}
	}
	return err
}

// musicCardTx runs block on a transactional builder, reusing the current one when the repository is
// already inside a transaction.
func musicCardTx(db dbx.Builder, block func(tx dbx.Builder) error) error {
	conn, ok := db.(*dbx.DB)
	if !ok {
		return block(db)
	}
	return conn.Transactional(func(tx *dbx.Tx) error { return block(tx) })
}

func (r *musicCardFolderRepository) withDB(db dbx.Builder) *musicCardFolderRepository {
	txr := *r
	txr.db = db
	return &txr
}

func (r *musicCardFolderRepository) EntityName() string {
	return "music_card_folder"
}

// NewInstance carries the review_enabled default: the REST layer decodes the body over it, so a
// payload omitting the flag creates a folder with review on.
func (r *musicCardFolderRepository) NewInstance() any {
	return &model.MusicCardFolder{ReviewEnabled: true}
}

func (r *musicCardFolderRepository) Read(id string) (any, error) {
	f, err := r.Get(id)
	if err != nil {
		// The generic REST controller only recognises its own not-found error.
		if errors.Is(err, model.ErrNotFound) {
			return nil, rest.ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (r *musicCardFolderRepository) ReadAll(options ...rest.QueryOptions) (any, error) {
	return r.GetAll(r.parseRestOptions(r.ctx, options...))
}

func (r *musicCardFolderRepository) Save(entity any) (string, error) {
	f := entity.(*model.MusicCardFolder)
	if err := r.Put(f); err != nil {
		return "", err
	}
	return f.ID, nil
}

// Update writes only the mutable columns, so neither ownership nor the default flag can be changed
// by a spoofed payload.
func (r *musicCardFolderRepository) Update(id string, entity any, cols ...string) error {
	f := entity.(*model.MusicCardFolder)
	f.ID = id
	f.Name = strings.TrimSpace(f.Name)
	if len(cols) == 0 {
		cols = slices.Clone(mutableMusicCardFolderCols)
	} else {
		cols = slices.DeleteFunc(slices.Clone(cols), func(c string) bool {
			return !slices.Contains(mutableMusicCardFolderCols, toSnakeCase(c))
		})
	}
	if slices.Contains(cols, "name") && f.Name == "" {
		return folderNameRequiredError()
	}
	f.UpdatedAt = time.Now()
	return folderNameConflict(r.updateOwned(id, f, append(cols, "updated_at")...))
}

var _ model.MusicCardFolderRepository = (*musicCardFolderRepository)(nil)
var _ rest.Repository = (*musicCardFolderRepository)(nil)
var _ rest.Persistable = (*musicCardFolderRepository)(nil)
