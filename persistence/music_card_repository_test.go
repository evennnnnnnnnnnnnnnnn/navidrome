package persistence

import (
	"context"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pocketbase/dbx"
)

var _ = Describe("MusicCardRepository", func() {
	var database *dbx.DB
	var adminRepo *musicCardRepository
	var regularRepo *musicCardRepository
	var thirdRepo *musicCardRepository

	repoAs := func(u model.User) *musicCardRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardRepository(ctx, database).(*musicCardRepository)
	}

	deleteAllOwnedBy := func(repo *musicCardRepository) {
		items, err := repo.GetAll()
		Expect(err).ToNot(HaveOccurred())
		for _, i := range items {
			_ = repo.Delete(i.ID)
		}
	}

	BeforeEach(func() {
		database = GetDBXBuilder()
		adminRepo = repoAs(adminUser)
		regularRepo = repoAs(regularUser)
		thirdRepo = repoAs(thirdUser)
	})

	AfterEach(func() {
		deleteAllOwnedBy(adminRepo)
		deleteAllOwnedBy(regularRepo)
		deleteAllOwnedBy(thirdRepo)
	})

	card := func() *model.MusicCard {
		return &model.MusicCard{KanjiText: "漢字"}
	}

	Describe("Put (upsert)", func() {
		It("creates a new card, forcing user_id from the auth context", func() {
			c := card()
			c.UserID = "attacker-supplied-id" // must be ignored
			Expect(regularRepo.Put(c)).To(Succeed())
			Expect(c.ID).ToNot(BeEmpty())
			Expect(c.UserID).To(Equal(regularUser.ID))

			got, err := regularRepo.Get(c.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.UserID).To(Equal(regularUser.ID))
			Expect(got.KanjiText).To(Equal("漢字"))
		})

		It("returns the existing card at the same (user, kanji_text) natural key instead of duplicating it", func() {
			c := card()
			Expect(regularRepo.Put(c)).To(Succeed())
			firstID := c.ID

			again := card()
			Expect(regularRepo.Put(again)).To(Succeed())
			Expect(again.ID).To(Equal(firstID), "upsert must keep the same row/id so a new context appends to the existing card")

			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1))
		})

		It("lets two different users bind the same kanji_text independently", func() {
			a := card()
			Expect(regularRepo.Put(a)).To(Succeed())

			b := card()
			Expect(thirdRepo.Put(b)).To(Succeed())

			Expect(a.ID).ToNot(Equal(b.ID))

			regularAll, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(regularAll).To(HaveLen(1))

			thirdAll, err := thirdRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(thirdAll).To(HaveLen(1))
		})
	})

	Describe("List", func() {
		It("scopes GetAll to the current user and supports filtering by kanji_text", func() {
			mine := card()
			Expect(regularRepo.Put(mine)).To(Succeed())

			otherKanji := card()
			otherKanji.KanjiText = "音楽"
			Expect(regularRepo.Put(otherKanji)).To(Succeed())

			someoneElses := card()
			Expect(thirdRepo.Put(someoneElses)).To(Succeed())

			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(2), "must not see thirdUser's card")

			filtered, err := regularRepo.GetAll(model.QueryOptions{Filters: eqFilter("kanji_text", "音楽")})
			Expect(err).ToNot(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].KanjiText).To(Equal("音楽"))
		})
	})

	Describe("Ownership enforcement", func() {
		var victim *model.MusicCard

		BeforeEach(func() {
			victim = card()
			Expect(regularRepo.Put(victim)).To(Succeed())
		})

		It("does not let another user read the card by id", func() {
			_, err := thirdRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let another user's GetAll include the card", func() {
			all, err := thirdRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(BeEmpty())
		})

		It("does not let another user delete the card by id, even by spoofing user_id", func() {
			err := thirdRepo.Delete(victim.ID)
			Expect(err).To(Equal(rest.ErrPermissionDenied))

			_, err = regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("does not let another user update the card by id, even by spoofing user_id in the payload", func() {
			spoofed := &model.MusicCard{
				ID:        victim.ID,
				UserID:    thirdUser.ID, // attacker's own id, spoofed
				KanjiText: "hijacked",
			}
			err := thirdRepo.Update(victim.ID, spoofed, "kanji_text")
			Expect(err).To(Equal(rest.ErrPermissionDenied))

			stillOwned, err := regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stillOwned.KanjiText).To(Equal(victim.KanjiText))
			Expect(stillOwned.UserID).To(Equal(regularUser.ID))
		})

		It("lets the owner delete their own card", func() {
			Expect(regularRepo.Delete(victim.ID)).To(Succeed())
			_, err := regularRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("lets an admin read and delete any user's card", func() {
			got, err := adminRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(victim.ID))

			Expect(adminRepo.Delete(victim.ID)).To(Succeed())
			_, err = regularRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("returns not-found when deleting a nonexistent card", func() {
			err := regularRepo.Delete("does-not-exist")
			Expect(err).To(Equal(rest.ErrNotFound))
		})

		It("cascades to delete the card's snippets when the card is deleted", func() {
			snippetRepo := NewMusicCardSnippetRepository(regularRepo.ctx, database).(*musicCardSnippetRepository)
			s := &model.MusicCardSnippet{
				CardID:      victim.ID,
				MediaFileID: songDayInALife.ID,
				LineIndex:   0,
				SnippetText: "line",
				Reading:     "reading",
				SongTitle:   "title",
				SongArtist:  "artist",
				FullLyrics:  "lyrics",
			}
			Expect(snippetRepo.Put(s)).To(Succeed())

			Expect(regularRepo.Delete(victim.ID)).To(Succeed())

			_, err := snippetRepo.Get(s.ID)
			Expect(err).To(Equal(model.ErrNotFound), "snippet must be gone once its card is deleted (FK cascade)")
		})
	})

	Describe("Save via the rest.Persistable interface", func() {
		It("upserts through Save exactly like Put", func() {
			c := card()
			id1, err := regularRepo.Save(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(id1).ToNot(BeEmpty())

			again := card()
			id2, err := regularRepo.Save(again)
			Expect(err).ToNot(HaveOccurred())
			Expect(id2).To(Equal(id1))
		})
	})

	Describe("Cascade delete", func() {
		It("removes cards when the owning user is deleted", func() {
			ctx := log.NewContext(context.TODO())
			ctx = request.WithUser(ctx, adminUser)
			userRepo := NewUserRepository(ctx, database)
			disposable := model.User{UserName: "music-card-cascade-test-user", Name: "Cascade Test", Email: "musiccard-cascade@example.com"}
			Expect(userRepo.Put(&disposable)).To(Succeed())

			c := card()
			Expect(repoAs(disposable).Put(c)).To(Succeed())

			Expect(userRepo.Delete(disposable.ID)).To(Succeed())

			_, err := adminRepo.Get(c.ID)
			Expect(err).To(Equal(model.ErrNotFound), "card must be gone once its owning user is deleted")
		})
	})

	Describe("EntityName/NewInstance", func() {
		It("returns the right entity name", func() {
			Expect(regularRepo.EntityName()).To(Equal("music_card"))
		})

		It("returns a new instance", func() {
			Expect(regularRepo.NewInstance()).To(Equal(&model.MusicCard{}))
		})
	})
})
