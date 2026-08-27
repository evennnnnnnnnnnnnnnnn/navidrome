package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upCreateLyricsOverrideTable, downCreateLyricsOverrideTable)
}

func upCreateLyricsOverrideTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
create table lyrics_override
(
    media_file_id varchar(255) not null
        references media_file
            on update cascade on delete cascade,
    lyrics     text not null,
    updated_by varchar(255),
    created_at datetime,
    updated_at datetime,
    constraint lyrics_override_pk
        primary key (media_file_id)
);
`)

	return err
}

func downCreateLyricsOverrideTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `drop table lyrics_override;`)
	return err
}
