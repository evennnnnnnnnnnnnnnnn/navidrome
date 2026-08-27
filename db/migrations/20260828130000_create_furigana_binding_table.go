package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upCreateFuriganaBindingTable, downCreateFuriganaBindingTable)
}

func upCreateFuriganaBindingTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
create table furigana_binding
(
    id            varchar(255) not null
        constraint furigana_binding_pk
            primary key,
    user_id       varchar(255) not null
        references user
            on update cascade on delete cascade,
    media_file_id varchar(255) not null
        references media_file
            on update cascade on delete cascade,
    line_index    integer not null,
    char_offset   integer not null,
    span_length   integer not null,
    kanji_text    text not null,
    reading       text not null,
    display       bool not null default true,
    created_at    datetime,
    updated_at    datetime,
    constraint furigana_binding_natural_key
        unique (user_id, media_file_id, line_index, char_offset)
);

create index if not exists idx_furigana_binding_media_file on furigana_binding (media_file_id);
`)

	return err
}

func downCreateFuriganaBindingTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `drop table furigana_binding;`)
	return err
}
