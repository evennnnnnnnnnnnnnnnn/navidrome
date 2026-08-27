package model

import (
	"encoding/json"
	"time"
)

// LyricsOverride is an admin-edited replacement for a media file's lyrics. It is
// track-scoped (not user-scoped): the stored lyrics win the resolution chain for
// every user and every Subsonic client. Lyrics is stored as model.LyricList JSON
// so it round-trips through buildLyricsList unchanged.
type LyricsOverride struct {
	MediaFileID string    `structs:"media_file_id" json:"mediaFileId"`
	Lyrics      string    `structs:"lyrics"         json:"lyrics"`
	UpdatedBy   string    `structs:"updated_by"     json:"updatedBy,omitempty"`
	CreatedAt   time.Time `structs:"created_at"     json:"createdAt"`
	UpdatedAt   time.Time `structs:"updated_at"     json:"updatedAt"`
}

// StructuredLyrics decodes the stored LyricList JSON.
func (o LyricsOverride) StructuredLyrics() (LyricList, error) {
	var list LyricList
	if err := json.Unmarshal([]byte(o.Lyrics), &list); err != nil {
		return nil, err
	}
	return list, nil
}

type LyricsOverrideRepository interface {
	// Get returns the override for a media file, or ErrNotFound if none exists.
	Get(mediaFileID string) (*LyricsOverride, error)
	// Put creates or replaces the override for a media file. Admin-only: returns
	// rest.ErrPermissionDenied for non-admin callers.
	Put(mediaFileID string, lyrics LyricList) error
	// Delete removes the override for a media file, restoring sidecar/embedded
	// resolution. Admin-only: returns rest.ErrPermissionDenied for non-admin callers.
	Delete(mediaFileID string) error
}
