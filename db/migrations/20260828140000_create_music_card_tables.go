package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upCreateMusicCardTables, downCreateMusicCardTables)
}

func upCreateMusicCardTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
create table music_card
(
    id         varchar(255) not null
        constraint music_card_pk
            primary key,
    user_id    varchar(255) not null
        references user
            on update cascade on delete cascade,
    kanji_text text    not null,
    created_at datetime,
    updated_at datetime,
    constraint music_card_natural_key
        unique (user_id, kanji_text)
);

create table music_card_snippet
(
    id            varchar(255) not null
        constraint music_card_snippet_pk
            primary key,
    card_id       varchar(255) not null
        references music_card
            on update cascade on delete cascade,
    media_file_id varchar(255) not null
        references media_file
            on update cascade on delete cascade,
    line_index    integer not null,
    char_offset   integer not null,
    span_length   integer not null,
    start_ms      integer not null,
    end_ms        integer not null,
    snippet_text  text    not null,
    reading       text    not null,
    song_title    text    not null,
    song_artist   text    not null,
    full_lyrics   text    not null,
    created_at    datetime,
    updated_at    datetime
);

create index if not exists idx_music_card_snippet_card on music_card_snippet (card_id);
create index if not exists idx_music_card_snippet_media_file on music_card_snippet (media_file_id);
`)

	return err
}

func downCreateMusicCardTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
drop table music_card_snippet;
drop table music_card;
`)
	return err
}
