package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddMusicCardSnippetWordFields, downAddMusicCardSnippetWordFields)
}

// The columns default to the empty string and are NOT NULL because the model fields are plain
// strings: database/sql cannot scan NULL into them, so legacy rows must read back as "" (unknown).
func upAddMusicCardSnippetWordFields(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
alter table music_card_snippet add column word_text text default '' not null;
alter table music_card_snippet add column word_base text default '' not null;
alter table music_card_snippet add column word_reading text default '' not null;
alter table music_card_snippet add column word_pos text default '' not null;
`)

	return err
}

func downAddMusicCardSnippetWordFields(_ context.Context, _ *sql.Tx) error {
	return nil
}
