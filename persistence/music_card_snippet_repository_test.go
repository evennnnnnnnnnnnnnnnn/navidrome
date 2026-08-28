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

var _ = Describe("MusicCardSnippetRepository", func() {
	var database *dbx.DB
	var adminCardRepo, regularCardRepo, thirdCardRepo *musicCardRepository
	var adminRepo, regularRepo, thirdRepo *musicCardSnippetRepository
	var regularCard, thirdCard *model.MusicCard

	cardRepoAs := func(u model.User) *musicCardRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardRepository(ctx, database).(*musicCardRepository)
	}

	snippetRepoAs := func(u model.User) *musicCardSnippetRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardSnippetRepository(ctx, database).(*musicCardSnippetRepository)
	}

	deleteAllOwnedBy := func(cardRepo *musicCardRepository) {
		items, err := cardRepo.GetAll()
		Expect(err).ToNot(HaveOccurred())
		for _, i := range items {
			_ = cardRepo.Delete(i.ID)
		}
	}

	BeforeEach(func() {
		database = GetDBXBuilder()
		adminCardRepo = cardRepoAs(adminUser)
		regularCardRepo = cardRepoAs(regularUser)
		thirdCardRepo = cardRepoAs(thirdUser)
		adminRepo = snippetRepoAs(adminUser)
		regularRepo = snippetRepoAs(regularUser)
		thirdRepo = snippetRepoAs(thirdUser)

		regularCard = &model.MusicCard{KanjiText: "漢字"}
		Expect(regularCardRepo.Put(regularCard)).To(Succeed())

		thirdCard = &model.MusicCard{KanjiText: "漢字"}
		Expect(thirdCardRepo.Put(thirdCard)).To(Succeed())
	})

	AfterEach(func() {
		deleteAllOwnedBy(adminCardRepo)
		deleteAllOwnedBy(regularCardRepo)
		deleteAllOwnedBy(thirdCardRepo)
	})

	snippet := func(cardID string) *model.MusicCardSnippet {
		return &model.MusicCardSnippet{
			CardID:      cardID,
			MediaFileID: songDayInALife.ID,
			LineIndex:   0,
			CharOffset:  3,
			SpanLength:  2,
			StartMs:     1000,
			EndMs:       3500,
			SnippetText: "歌詞の行",
			Reading:     "かしのぎょう",
			SongTitle:   "A Day In A Life",
			SongArtist:  "The Beatles",
			FullLyrics:  "full lyrics text",
		}
	}

	Describe("Put (create)", func() {
		It("creates a snippet on a card the caller owns", func() {
			s := snippet(regularCard.ID)
			Expect(regularRepo.Put(s)).To(Succeed())
			Expect(s.ID).ToNot(BeEmpty())

			got, err := regularRepo.Get(s.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardID).To(Equal(regularCard.ID))
			Expect(got.SnippetText).To(Equal("歌詞の行"))
		})

		It("refuses to attach a snippet to another user's card, even when card_id is spoofed", func() {
			s := snippet(thirdCard.ID) // regularUser attempting to attach to thirdUser's card
			err := regularRepo.Put(s)
			Expect(err).To(Equal(rest.ErrPermissionDenied))
		})

		It("returns not-found when the target card doesn't exist", func() {
			s := snippet("does-not-exist")
			err := regularRepo.Put(s)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("lets an admin attach a snippet to any user's card", func() {
			s := snippet(regularCard.ID)
			Expect(adminRepo.Put(s)).To(Succeed())
			Expect(s.ID).ToNot(BeEmpty())
		})
	})

	Describe("List by card/media file", func() {
		It("scopes GetAll to snippets on cards the caller owns and supports filtering by card_id and media_file_id", func() {
			mine := snippet(regularCard.ID)
			Expect(regularRepo.Put(mine)).To(Succeed())

			otherSong := snippet(regularCard.ID)
			otherSong.MediaFileID = songComeTogether.ID
			Expect(regularRepo.Put(otherSong)).To(Succeed())

			someoneElses := snippet(thirdCard.ID)
			Expect(thirdRepo.Put(someoneElses)).To(Succeed())

			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(2), "must not see thirdUser's snippet")

			filtered, err := regularRepo.GetAll(model.QueryOptions{Filters: eqFilter("media_file_id", songComeTogether.ID)})
			Expect(err).ToNot(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].MediaFileID).To(Equal(songComeTogether.ID))

			byCard, err := regularRepo.GetAll(model.QueryOptions{Filters: eqFilter("card_id", regularCard.ID)})
			Expect(err).ToNot(HaveOccurred())
			Expect(byCard).To(HaveLen(2))
		})
	})

	Describe("Ownership enforcement", func() {
		var victim *model.MusicCardSnippet

		BeforeEach(func() {
			victim = snippet(regularCard.ID)
			Expect(regularRepo.Put(victim)).To(Succeed())
		})

		It("does not let another user read the snippet by id", func() {
			_, err := thirdRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let another user's GetAll include the snippet", func() {
			all, err := thirdRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(BeEmpty())
		})

		It("does not let another user update the snippet by id, even by spoofing card_id in the payload", func() {
			spoofed := &model.MusicCardSnippet{
				ID:          victim.ID,
				CardID:      thirdCard.ID, // attacker's own card, spoofed
				MediaFileID: victim.MediaFileID,
				SnippetText: "hijacked",
			}
			err := thirdRepo.Update(victim.ID, spoofed, "snippet_text")
			Expect(err).To(Equal(rest.ErrPermissionDenied))

			stillOwned, err := regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stillOwned.SnippetText).To(Equal(victim.SnippetText))
			Expect(stillOwned.CardID).To(Equal(regularCard.ID))
		})

		It("does not let another user delete the snippet by id", func() {
			err := thirdRepo.Delete(victim.ID)
			Expect(err).To(Equal(rest.ErrPermissionDenied))

			_, err = regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("lets the owner update their own snippet, without reassigning card_id via payload", func() {
			reassign := &model.MusicCardSnippet{
				ID:          victim.ID,
				CardID:      thirdCard.ID, // attempted move, must be ignored
				MediaFileID: victim.MediaFileID,
				SnippetText: "renamed-by-owner",
			}
			err := regularRepo.Update(victim.ID, reassign, "snippet_text", "card_id")
			Expect(err).ToNot(HaveOccurred())

			stored, err := regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.SnippetText).To(Equal("renamed-by-owner"))
			Expect(stored.CardID).To(Equal(regularCard.ID))
		})

		It("lets the owner delete their own snippet", func() {
			Expect(regularRepo.Delete(victim.ID)).To(Succeed())
			_, err := regularRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("lets an admin read and delete any user's snippet", func() {
			got, err := adminRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(victim.ID))

			Expect(adminRepo.Delete(victim.ID)).To(Succeed())
			_, err = regularRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("returns not-found when deleting a nonexistent snippet", func() {
			err := regularRepo.Delete("does-not-exist")
			Expect(err).To(Equal(rest.ErrNotFound))
		})
	})

	Describe("Save via the rest.Persistable interface", func() {
		It("creates through Save exactly like Put", func() {
			s := snippet(regularCard.ID)
			id1, err := regularRepo.Save(s)
			Expect(err).ToNot(HaveOccurred())
			Expect(id1).ToNot(BeEmpty())
		})
	})

	Describe("Cascade delete", func() {
		It("removes snippets when the parent card is deleted", func() {
			s := snippet(regularCard.ID)
			Expect(regularRepo.Put(s)).To(Succeed())

			Expect(regularCardRepo.Delete(regularCard.ID)).To(Succeed())

			_, err := adminRepo.Get(s.ID)
			Expect(err).To(Equal(model.ErrNotFound), "snippet must be gone once its card is deleted")
		})

		It("removes snippets when the referenced media file is deleted", func() {
			mediaFileRepo := NewMediaFileRepository(regularRepo.ctx, database)
			mf := songDayInALife
			mf.ID = "music-card-snippet-cascade-mf"
			mf.Path = "beatles/1/sgt/cascade-test.mp3"
			Expect(mediaFileRepo.Put(&mf)).To(Succeed())

			s := snippet(regularCard.ID)
			s.MediaFileID = mf.ID
			Expect(regularRepo.Put(s)).To(Succeed())

			Expect(mediaFileRepo.Delete(mf.ID)).To(Succeed())

			_, err := adminRepo.Get(s.ID)
			Expect(err).To(Equal(model.ErrNotFound), "snippet must be gone once its media file is deleted")
		})
	})

	Describe("EntityName/NewInstance", func() {
		It("returns the right entity name", func() {
			Expect(regularRepo.EntityName()).To(Equal("music_card_snippet"))
		})

		It("returns a new instance", func() {
			Expect(regularRepo.NewInstance()).To(Equal(&model.MusicCardSnippet{}))
		})
	})
})
