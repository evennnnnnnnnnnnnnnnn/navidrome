package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upCreateMusicCardReviewTable, downCreateMusicCardReviewTable)
}

func upCreateMusicCardReviewTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
create table music_card_review
(
    id               varchar(255) not null
        constraint music_card_review_pk
            primary key,
    card_id          varchar(255) not null
        references music_card
            on update cascade on delete cascade,
    due_at           datetime not null,
    interval_days    real     not null default 0,
    ease_factor      real     not null default 2.5,
    repetition_count integer  not null default 0,
    lapse_count      integer  not null default 0,
    last_reviewed_at datetime,
    created_at       datetime,
    updated_at       datetime,
    constraint music_card_review_card_key
        unique (card_id)
);

create index if not exists idx_music_card_review_due on music_card_review (due_at);
`)

	return err
}

func downCreateMusicCardReviewTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
drop table music_card_review;
`)
	return err
}
