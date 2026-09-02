package migrations

import (
	"context"
	"database/sql"
	"time"

	"github.com/navidrome/navidrome/model/id"
	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upCreateMusicCardFolderTables, downCreateMusicCardFolderTables)
}

func upCreateMusicCardFolderTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
create table music_card_folder
(
    id             varchar(255) not null
        constraint music_card_folder_pk
            primary key,
    user_id        varchar(255) not null
        references user
            on update cascade on delete cascade,
    name           text    not null,
    public         bool    not null default false,
    review_enabled bool    not null default true,
    is_default     bool    not null default false,
    created_at     datetime,
    updated_at     datetime,
    constraint music_card_folder_natural_key
        unique (user_id, name)
);

create unique index if not exists idx_music_card_folder_default on music_card_folder (user_id) where is_default;

create table music_card_folder_card
(
    folder_id  varchar(255) not null
        references music_card_folder
            on update cascade on delete cascade,
    card_id    varchar(255) not null
        references music_card
            on update cascade on delete cascade,
    created_at datetime,
    constraint music_card_folder_card_pk
        primary key (folder_id, card_id)
);

create index if not exists idx_music_card_folder_card_card on music_card_folder_card (card_id);
`)
	if err != nil {
		return err
	}
	return backfillMusicCardDefaultFolders(ctx, tx)
}

// backfillMusicCardDefaultFolders gives every existing user a default folder and drops every
// existing card into its owner's, so the deck a user already had becomes their default deck.
func backfillMusicCardDefaultFolders(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `select id from user`)
	if err != nil {
		return err
	}
	var userIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	now := time.Now()
	for _, userID := range userIDs {
		_, err = tx.ExecContext(ctx, `
insert into music_card_folder (id, user_id, name, public, review_enabled, is_default, created_at, updated_at)
values (?, ?, 'Default', false, true, true, ?, ?)`, id.NewRandom(), userID, now, now)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
insert into music_card_folder_card (folder_id, card_id, created_at)
select f.id, c.id, ?
  from music_card c
  join music_card_folder f on f.user_id = c.user_id and f.is_default`, now)
	return err
}

func downCreateMusicCardFolderTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
drop table music_card_folder_card;
drop table music_card_folder;
`)
	return err
}
