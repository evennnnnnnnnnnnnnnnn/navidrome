package model

import "time"

// MusicCardFolder is a user-owned deck of MusicCards, shaped like a Playlist: membership is a join
// table so a card can sit in several folders, and a public folder is readable by every other user
// on the server while staying writable only by its owner. Every user has exactly one is_default
// folder - the deck new cards land in - which cannot be deleted.
type MusicCardFolder struct {
	ID            string    `structs:"id"             json:"id"`
	UserID        string    `structs:"user_id"        json:"user_id"`
	OwnerName     string    `structs:"-"              json:"owner_name"`
	Name          string    `structs:"name"           json:"name"`
	Public        bool      `structs:"public"         json:"public"`
	ReviewEnabled bool      `structs:"review_enabled" json:"review_enabled"`
	IsDefault     bool      `structs:"is_default"     json:"is_default"`
	CardIDs       []string  `structs:"-"              json:"card_ids"`
	CardCount     int       `structs:"-"              json:"card_count"`
	CreatedAt     time.Time `structs:"created_at"     json:"created_at"`
	UpdatedAt     time.Time `structs:"updated_at"     json:"updated_at"`
}

type MusicCardFolders []MusicCardFolder

// MusicCardWithSnippets is a card served with its snippets in one response, so a viewer reading
// someone else's public folder can render and replay every card without a second round trip (and
// without any cross-user read on the card/snippet repositories themselves).
type MusicCardWithSnippets struct {
	MusicCard
	Snippets MusicCardSnippets `json:"snippets"`
}

type MusicCardsWithSnippets []MusicCardWithSnippets

// MusicCardFolderRepository reads under "owned by me or public" and writes owner-only: no admin
// bypass on either side, and user_id/is_default are always server-owned, never taken from a
// request payload.
type MusicCardFolderRepository interface {
	ResourceRepository
	CountAll(options ...QueryOptions) (int64, error)
	Get(id string) (*MusicCardFolder, error)
	GetAll(options ...QueryOptions) (MusicCardFolders, error)
	Put(f *MusicCardFolder) error
	Delete(id string) error
	// EnsureDefault returns the caller's default folder, creating it on first use.
	EnsureDefault() (*MusicCardFolder, error)
	// AddCardToDefault places a newly created card in its owner's default folder.
	AddCardToDefault(cardID string) error
	// AddCards is idempotent and all-or-nothing: every card must be owned by the caller, or the
	// whole call fails with rest.ErrPermissionDenied.
	AddCards(folderID string, cardIDs []string) error
	RemoveCard(folderID, cardID string) error
	// Cards returns the folder's cards with their snippets when the folder is visible to the
	// caller, and ErrNotFound when it is not - the one deliberate cross-user read in the card
	// system.
	Cards(folderID string) (MusicCardsWithSnippets, error)
}
