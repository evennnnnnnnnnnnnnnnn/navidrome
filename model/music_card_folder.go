package model

import "time"

// MusicCardFolder is a user-owned deck of MusicCards. Membership is a join table, so a card can sit
// in several folders.
// Wire fields are snake_case to match the client-side schema 1:1.
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

// MusicCardWithSnippets lets a viewer render and replay a public folder without a second round trip
// or any cross-user read on the card/snippet repositories.
type MusicCardWithSnippets struct {
	MusicCard
	Snippets MusicCardSnippets `json:"snippets"`
}

type MusicCardsWithSnippets []MusicCardWithSnippets

// MusicCardFolderRepository reads "owned by me or public" and writes owner-only, with no admin
// bypass on either side.
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
