package migrations

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("upCreateMusicCardFolderTables", func() {
	var db *sql.DB
	ctx := context.Background()

	exec := func(query string, args ...any) {
		_, err := db.ExecContext(ctx, query, args...)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
	}

	scalar := func(query string, args ...any) int {
		var n int
		ExpectWithOffset(1, db.QueryRowContext(ctx, query, args...).Scan(&n)).To(Succeed())
		return n
	}

	defaultFolderOf := func(userID string) string {
		var id string
		ExpectWithOffset(1, db.QueryRowContext(ctx,
			`select id from music_card_folder where user_id = ? and is_default`, userID).Scan(&id)).To(Succeed())
		return id
	}

	runUp := func() {
		tx, err := db.BeginTx(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(upCreateMusicCardFolderTables(ctx, tx)).To(Succeed())
		Expect(tx.Commit()).To(Succeed())
	}

	BeforeEach(func() {
		var err error
		db, err = sql.Open("sqlite3", "file::memory:")
		Expect(err).ToNot(HaveOccurred())
		db.SetMaxOpenConns(1) // non-shared :memory: — every new conn would be a fresh empty DB
		DeferCleanup(func() { _ = db.Close() })

		exec(`create table user (id varchar(255) primary key, user_name text)`)
		exec(`create table music_card (id varchar(255) primary key, user_id varchar(255), kanji_text text)`)
		exec(`insert into user values ('u1', 'alice'), ('u2', 'bob'), ('u3', 'carol')`)
		exec(`insert into music_card values ('c1', 'u1', '漢'), ('c2', 'u1', '字'), ('c3', 'u2', '音')`)
	})

	It("gives every existing user exactly one default folder named Default", func() {
		runUp()

		Expect(scalar(`select count(*) from music_card_folder`)).To(Equal(3))
		Expect(scalar(`select count(*) from music_card_folder
			where name = 'Default' and is_default and review_enabled and not public`)).To(Equal(3))
		Expect(scalar(`select count(distinct id) from music_card_folder`)).To(Equal(3))
	})

	It("puts every existing card in its owner's default folder", func() {
		runUp()

		Expect(scalar(`select count(*) from music_card_folder_card`)).To(Equal(3))
		Expect(scalar(`select count(*) from music_card_folder_card where folder_id = ?`, defaultFolderOf("u1"))).To(Equal(2))
		Expect(scalar(`select count(*) from music_card_folder_card where folder_id = ?`, defaultFolderOf("u2"))).To(Equal(1))
		Expect(scalar(`select count(*) from music_card_folder_card where folder_id = ?`, defaultFolderOf("u3"))).To(BeZero())
		Expect(scalar(`select count(*) from music_card_folder_card fc
			join music_card c on c.id = fc.card_id
			join music_card_folder f on f.id = fc.folder_id
			where f.user_id != c.user_id`)).To(BeZero())
	})

	It("keeps the default folder single through the partial unique index", func() {
		runUp()

		_, err := db.ExecContext(ctx, `insert into music_card_folder (id, user_id, name, public, review_enabled, is_default)
			values ('dup', 'u1', 'Second default', false, true, true)`)
		Expect(err).To(HaveOccurred())

		// a second non-default folder is fine, a duplicate name is not
		exec(`insert into music_card_folder (id, user_id, name, public, review_enabled, is_default)
			values ('extra', 'u1', 'JLPT N3', false, true, false)`)
		_, err = db.ExecContext(ctx, `insert into music_card_folder (id, user_id, name, public, review_enabled, is_default)
			values ('extra2', 'u1', 'JLPT N3', false, true, false)`)
		Expect(err).To(HaveOccurred())
	})

	It("drops both tables on down", func() {
		runUp()

		tx, err := db.BeginTx(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(downCreateMusicCardFolderTables(ctx, tx)).To(Succeed())
		Expect(tx.Commit()).To(Succeed())

		Expect(scalar(`select count(*) from sqlite_master
			where type = 'table' and name in ('music_card_folder', 'music_card_folder_card')`)).To(BeZero())
	})
})
