package model

import "time"

// MusicCard is a per-user Anki-style revision card keyed on a kanji run (a single kanji or an
// adjacent pair). It has no content of its own beyond the identity key - all lyric/audio content
// lives on its MusicCardSnippet rows, so saving the same kanji run from a different song or line
// upserts into the existing card instead of creating a duplicate.
type MusicCard struct {
	ID        string    `structs:"id"         json:"id"`
	UserID    string    `structs:"user_id"    json:"user_id"`
	KanjiText string    `structs:"kanji_text" json:"kanji_text"`
	CreatedAt time.Time `structs:"created_at" json:"created_at"`
	UpdatedAt time.Time `structs:"updated_at" json:"updated_at"`
}

type MusicCards []MusicCard

// MusicCardRepository is a per-user resource repository: every method must scope reads and writes
// to the authenticated user (loggedUser(ctx)), never trusting a user_id supplied in the request
// payload.
type MusicCardRepository interface {
	ResourceRepository
	CountAll(options ...QueryOptions) (int64, error)
	Get(id string) (*MusicCard, error)
	GetAll(options ...QueryOptions) (MusicCards, error)
	// Put creates a new card, or returns the existing one at the same (user, kanji_text) natural
	// key - the upsert semantics the native REST route exposes.
	Put(mc *MusicCard) error
	Delete(id string) error
}
