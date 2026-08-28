package model

import "time"

// MusicCardSnippet is one context snippet on a MusicCard: a 1-2 lyric line span with ms-precision
// timing, the bound reading, and a full-lyrics snapshot, so the card never needs to re-resolve
// lyrics or bindings from the source song once saved. It has no user_id column of its own -
// ownership is transitive through CardID, matching the (user, kanji_text) card it belongs to.
type MusicCardSnippet struct {
	ID          string    `structs:"id"            json:"id"`
	CardID      string    `structs:"card_id"       json:"card_id"`
	MediaFileID string    `structs:"media_file_id" json:"media_file_id"`
	LineIndex   int       `structs:"line_index"    json:"line_index"`
	CharOffset  int       `structs:"char_offset"   json:"char_offset"`
	SpanLength  int       `structs:"span_length"   json:"span_length"`
	StartMs     int       `structs:"start_ms"      json:"start_ms"`
	EndMs       int       `structs:"end_ms"        json:"end_ms"`
	SnippetText string    `structs:"snippet_text"  json:"snippet_text"`
	Reading     string    `structs:"reading"       json:"reading"`
	SongTitle   string    `structs:"song_title"    json:"song_title"`
	SongArtist  string    `structs:"song_artist"   json:"song_artist"`
	FullLyrics  string    `structs:"full_lyrics"   json:"full_lyrics"`
	CreatedAt   time.Time `structs:"created_at"    json:"created_at"`
	UpdatedAt   time.Time `structs:"updated_at"    json:"updated_at"`
}

type MusicCardSnippets []MusicCardSnippet

// MusicCardSnippetRepository is a resource repository whose ownership is scoped transitively
// through the parent MusicCard's user_id: every method must verify the caller owns (or is admin
// of) CardID's card, never trusting a card_id supplied in the request payload to bypass that check.
type MusicCardSnippetRepository interface {
	ResourceRepository
	CountAll(options ...QueryOptions) (int64, error)
	Get(id string) (*MusicCardSnippet, error)
	GetAll(options ...QueryOptions) (MusicCardSnippets, error)
	Put(s *MusicCardSnippet) error
	Delete(id string) error
}
