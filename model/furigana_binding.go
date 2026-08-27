package model

import "time"

// FuriganaBinding is a per-user reading binding for a kanji span within a line of a song's
// lyrics. Codepoint offsets (CharOffset, SpanLength) and KanjiText (stale-anchor detection)
// mirror the Museeks sidecar schema so client-side render/align logic ports unchanged. Wire
// fields are snake_case to match that schema 1:1.
type FuriganaBinding struct {
	ID          string    `structs:"id"            json:"id"`
	UserID      string    `structs:"user_id"       json:"user_id"`
	MediaFileID string    `structs:"media_file_id" json:"media_file_id"`
	LineIndex   int       `structs:"line_index"    json:"line_index"`
	CharOffset  int       `structs:"char_offset"   json:"char_offset"`
	SpanLength  int       `structs:"span_length"   json:"span_length"`
	KanjiText   string    `structs:"kanji_text"    json:"kanji_text"`
	Reading     string    `structs:"reading"       json:"reading"`
	Display     bool      `structs:"display"       json:"display"`
	CreatedAt   time.Time `structs:"created_at"    json:"created_at"`
	UpdatedAt   time.Time `structs:"updated_at"    json:"updated_at"`
}

type FuriganaBindings []FuriganaBinding

// FuriganaBindingRepository is a per-user resource repository: every method must scope reads
// and writes to the authenticated user (loggedUser(ctx) / request.UserFrom(ctx)), never trusting
// a user_id supplied in the request payload.
type FuriganaBindingRepository interface {
	ResourceRepository
	CountAll(options ...QueryOptions) (int64, error)
	Get(id string) (*FuriganaBinding, error)
	GetAll(options ...QueryOptions) (FuriganaBindings, error)
	// Put creates a new binding, or replaces the existing one at the same (user, song, line,
	// char offset) natural key - the upsert semantics the native REST route exposes.
	Put(fb *FuriganaBinding) error
	Delete(id string) error
}
