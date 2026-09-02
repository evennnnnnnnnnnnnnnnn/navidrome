package persistence

import (
	"context"
	"errors"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pocketbase/dbx"
)

func countDefaultFoldersOf(database *dbx.DB, userID string) int {
	var count int
	err := database.NewQuery("select count(*) from music_card_folder where user_id={:u} and is_default").
		Bind(dbx.Params{"u": userID}).Row(&count)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return count
}

var _ = Describe("MusicCardFolderRepository", func() {
	var database *dbx.DB
	var adminRepo *musicCardFolderRepository
	var regularRepo *musicCardFolderRepository
	var thirdRepo *musicCardFolderRepository

	repoAs := func(u model.User) *musicCardFolderRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardFolderRepository(ctx, database).(*musicCardFolderRepository)
	}

	cardRepoAs := func(u model.User) *musicCardRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardRepository(ctx, database).(*musicCardRepository)
	}

	wipe := func() {
		for _, table := range []string{"music_card_folder_card", "music_card_folder", "music_card"} {
			_, err := database.NewQuery("delete from " + table).Execute()
			Expect(err).ToNot(HaveOccurred())
		}
	}

	BeforeEach(func() {
		database = GetDBXBuilder()
		wipe()
		adminRepo = repoAs(adminUser)
		regularRepo = repoAs(regularUser)
		thirdRepo = repoAs(thirdUser)
	})

	AfterEach(wipe)

	folder := func(name string) *model.MusicCardFolder {
		return &model.MusicCardFolder{Name: name, ReviewEnabled: true}
	}

	newCard := func(repo *musicCardRepository, kanji string) *model.MusicCard {
		c := &model.MusicCard{KanjiText: kanji}
		Expect(repo.Put(c)).To(Succeed())
		return c
	}

	Describe("Put", func() {
		It("creates a folder, forcing user_id from the auth context and ignoring is_default", func() {
			f := folder("JLPT N3")
			f.UserID = "attacker-supplied-id"
			f.IsDefault = true
			Expect(regularRepo.Put(f)).To(Succeed())

			got, err := regularRepo.Get(f.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.UserID).To(Equal(regularUser.ID))
			Expect(got.IsDefault).To(BeFalse())
			Expect(got.Public).To(BeFalse())
			Expect(got.ReviewEnabled).To(BeTrue())
			Expect(got.OwnerName).To(Equal(regularUser.UserName))
			Expect(got.CardIDs).To(BeEmpty())
			Expect(got.CardCount).To(BeZero())
		})

		It("trims the name and refuses an empty one", func() {
			f := folder("  Songs by X  ")
			Expect(regularRepo.Put(f)).To(Succeed())
			Expect(f.Name).To(Equal("Songs by X"))

			var verr *rest.ValidationError
			err := regularRepo.Put(folder("   "))
			Expect(errors.As(err, &verr)).To(BeTrue())
		})

		It("refuses a duplicate name with a unique validation error, on create and on rename", func() {
			Expect(regularRepo.Put(folder("JLPT N3"))).To(Succeed())
			other := folder("JLPT N4")
			Expect(regularRepo.Put(other)).To(Succeed())

			var verr *rest.ValidationError
			err := regularRepo.Put(folder("JLPT N3"))
			Expect(errors.As(err, &verr)).To(BeTrue())
			Expect(verr.Errors).To(HaveKeyWithValue("name", "ra.validation.unique"))

			err = regularRepo.Update(other.ID, &model.MusicCardFolder{Name: "JLPT N3"}, "name")
			Expect(errors.As(err, &verr)).To(BeTrue())
			Expect(verr.Errors).To(HaveKeyWithValue("name", "ra.validation.unique"))

			Expect(thirdRepo.Put(folder("JLPT N3"))).To(Succeed(), "the name is unique per user, not per server")
		})

		It("materialises the default deck first, so its reserved name cannot be squatted", func() {
			var verr *rest.ValidationError
			err := regularRepo.Put(folder(defaultMusicCardFolderName))
			Expect(errors.As(err, &verr)).To(BeTrue())
			Expect(countDefaultFoldersOf(database, regularUser.ID)).To(Equal(1))
		})

		It("upserts through Save exactly like Put", func() {
			f := folder("Saved")
			id, err := regularRepo.Save(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(f.ID))
		})
	})

	Describe("Visibility", func() {
		var private, shared *model.MusicCardFolder

		BeforeEach(func() {
			private = folder("Private deck")
			Expect(regularRepo.Put(private)).To(Succeed())
			shared = folder("Shared deck")
			shared.Public = true
			Expect(regularRepo.Put(shared)).To(Succeed())
		})

		It("lets the owner see both of their folders", func() {
			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			names := []string{}
			for _, f := range all {
				names = append(names, f.Name)
			}
			Expect(names).To(ContainElements("Private deck", "Shared deck"))
		})

		It("lets a third user see only the public folder, with the owner's name", func() {
			got, err := thirdRepo.Get(shared.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.OwnerName).To(Equal(regularUser.UserName))

			_, err = thirdRepo.Get(private.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let an admin see another user's private folder", func() {
			_, err := adminRepo.Get(private.ID)
			Expect(err).To(Equal(model.ErrNotFound), "folders are private study data - admins are scoped like everyone else")

			all, err := adminRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			for _, f := range all {
				Expect(f.ID).ToNot(Equal(private.ID))
			}
		})

		It("orders the default deck first, then by name", func() {
			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all[0].IsDefault).To(BeTrue())
			Expect(all[0].UserID).To(Equal(regularUser.ID))
		})

		It("counts only the folders visible to the caller", func() {
			all, err := thirdRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			for _, f := range all {
				Expect(f.Public || f.UserID == thirdUser.ID).To(BeTrue())
			}

			count, err := thirdRepo.CountAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(int64(len(all))))
		})
	})

	Describe("Ownership enforcement", func() {
		var shared *model.MusicCardFolder

		BeforeEach(func() {
			shared = folder("Shared deck")
			shared.Public = true
			Expect(regularRepo.Put(shared)).To(Succeed())
		})

		It("does not let a non-owner rename or share a public folder", func() {
			hijack := &model.MusicCardFolder{Name: "hijacked", Public: false, UserID: thirdUser.ID, IsDefault: true}
			Expect(thirdRepo.Update(shared.ID, hijack, "name", "public")).To(Equal(rest.ErrPermissionDenied))

			survived, err := regularRepo.Get(shared.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(survived.Name).To(Equal("Shared deck"))
			Expect(survived.Public).To(BeTrue())
			Expect(survived.UserID).To(Equal(regularUser.ID))
		})

		It("does not let a non-owner delete a public folder", func() {
			Expect(thirdRepo.Delete(shared.ID)).To(Equal(rest.ErrPermissionDenied))
			_, err := regularRepo.Get(shared.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("does not let an admin modify another user's public folder", func() {
			Expect(adminRepo.Delete(shared.ID)).To(Equal(rest.ErrPermissionDenied))
		})

		It("never writes user_id or is_default on update", func() {
			payload := &model.MusicCardFolder{Name: "Renamed", UserID: thirdUser.ID, IsDefault: true}
			Expect(regularRepo.Update(shared.ID, payload, "name", "user_id", "is_default")).To(Succeed())

			got, err := regularRepo.Get(shared.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Name).To(Equal("Renamed"))
			Expect(got.UserID).To(Equal(regularUser.ID))
			Expect(got.IsDefault).To(BeFalse())
		})

		It("refuses an empty name on update", func() {
			var verr *rest.ValidationError
			err := regularRepo.Update(shared.ID, &model.MusicCardFolder{Name: "  "}, "name")
			Expect(errors.As(err, &verr)).To(BeTrue())
		})

		It("returns not-found when deleting a nonexistent folder", func() {
			Expect(regularRepo.Delete("does-not-exist")).To(Equal(rest.ErrNotFound))
		})
	})

	Describe("Default folder", func() {
		It("creates the caller's default folder once and returns the same one after that", func() {
			first, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())
			Expect(first.IsDefault).To(BeTrue())
			Expect(first.Name).To(Equal("Default"))
			Expect(first.ReviewEnabled).To(BeTrue())

			again, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())
			Expect(again.ID).To(Equal(first.ID))

			Expect(countDefaultFoldersOf(database, regularUser.ID)).To(Equal(1))
		})

		It("is kept single by the partial unique index, even against a direct second insert", func() {
			def, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())

			_, err = database.NewQuery(`insert into music_card_folder (id, user_id, name, public, review_enabled, is_default)
				values ('dup-default', {:u}, 'Second default', false, true, true)`).
				Bind(dbx.Params{"u": regularUser.ID}).Execute()
			Expect(err).To(HaveOccurred())
			Expect(countDefaultFoldersOf(database, regularUser.ID)).To(Equal(1))
			Expect(def.ID).ToNot(Equal("dup-default"))
		})

		It("cannot be deleted", func() {
			def, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())

			var verr *rest.ValidationError
			err = regularRepo.Delete(def.ID)
			Expect(errors.As(err, &verr)).To(BeTrue())

			_, err = regularRepo.Get(def.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can still be renamed, shared and review-toggled", func() {
			def, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())

			payload := &model.MusicCardFolder{Name: "My deck", Public: true, ReviewEnabled: false}
			Expect(regularRepo.Update(def.ID, payload, "name", "public", "review_enabled")).To(Succeed())

			got, err := regularRepo.Get(def.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Name).To(Equal("My deck"))
			Expect(got.Public).To(BeTrue())
			Expect(got.ReviewEnabled).To(BeFalse())
			Expect(got.IsDefault).To(BeTrue())
		})

		It("gives each user their own default folder", func() {
			mine, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())
			theirs, err := thirdRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())
			Expect(mine.ID).ToNot(Equal(theirs.ID))
		})

		It("returns the winner's folder when the default was created between the lookup and the insert", func() {
			winner, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())

			// insertDefault is the branch a caller reaches once its own lookup missed; the partial
			// unique index makes its insert fail exactly as it does in the race.
			got, err := regularRepo.insertDefault(regularUser.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(winner.ID))
			Expect(countDefaultFoldersOf(database, regularUser.ID)).To(Equal(1))
		})

		It("rolls the new card back when its default-deck membership cannot be written", func() {
			// A folder already holding the reserved name makes lazy default creation fail.
			_, err := database.NewQuery(`insert into music_card_folder (id, user_id, name, public, review_enabled, is_default)
				values ('squatter', {:u}, 'Default', false, true, false)`).
				Bind(dbx.Params{"u": regularUser.ID}).Execute()
			Expect(err).ToNot(HaveOccurred())

			cardRepo := cardRepoAs(regularUser)
			Expect(cardRepo.Put(&model.MusicCard{KanjiText: "巻戻"})).ToNot(Succeed())

			cards, err := cardRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(cards).To(BeEmpty(), "a card stored without its membership would never get one")
		})

		It("places a newly created card in its owner's default folder, and leaves an upsert alone", func() {
			cardRepo := cardRepoAs(regularUser)
			c := newCard(cardRepo, "漢字")

			def, err := regularRepo.EnsureDefault()
			Expect(err).ToNot(HaveOccurred())
			Expect(def.CardIDs).To(BeNil()) // EnsureDefault is a lean lookup; ids come from Get/GetAll

			got, err := regularRepo.Get(def.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(ConsistOf(c.ID))
			Expect(got.CardCount).To(Equal(1))

			Expect(regularRepo.RemoveCard(def.ID, c.ID)).To(Succeed())
			Expect(cardRepo.Put(&model.MusicCard{KanjiText: "漢字"})).To(Succeed())

			got, err = regularRepo.Get(def.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(BeEmpty(), "an upsert into an existing card must not put it back in the default deck")
		})
	})

	Describe("Membership", func() {
		var deck *model.MusicCardFolder
		var mine *model.MusicCard

		BeforeEach(func() {
			deck = folder("Study deck")
			Expect(regularRepo.Put(deck)).To(Succeed())
			mine = newCard(cardRepoAs(regularUser), "音楽")
		})

		It("adds cards idempotently", func() {
			Expect(regularRepo.AddCards(deck.ID, []string{mine.ID, mine.ID})).To(Succeed())
			Expect(regularRepo.AddCards(deck.ID, []string{mine.ID})).To(Succeed())

			got, err := regularRepo.Get(deck.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(ConsistOf(mine.ID))
		})

		It("adds cards in the caller's transaction, so a rollback leaves no membership behind", func() {
			err := database.Transactional(func(tx *dbx.Tx) error {
				repo := NewMusicCardFolderRepository(regularRepo.ctx, tx)
				Expect(repo.AddCards(deck.ID, []string{mine.ID})).To(Succeed())
				return errors.New("rolled back")
			})
			Expect(err).To(MatchError("rolled back"))

			got, err := regularRepo.Get(deck.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(BeEmpty())
		})

		It("refuses the whole call when any card belongs to another user", func() {
			theirs := newCard(cardRepoAs(thirdUser), "音楽")

			Expect(regularRepo.AddCards(deck.ID, []string{mine.ID, theirs.ID})).To(Equal(rest.ErrPermissionDenied))

			got, err := regularRepo.Get(deck.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(BeEmpty(), "the refusal must be all-or-nothing")
		})

		It("does not let a non-owner add to or remove from a public folder", func() {
			shared := folder("Shared deck")
			shared.Public = true
			Expect(regularRepo.Put(shared)).To(Succeed())
			theirs := newCard(cardRepoAs(thirdUser), "楽器")

			Expect(thirdRepo.AddCards(shared.ID, []string{theirs.ID})).To(Equal(rest.ErrPermissionDenied))
			Expect(thirdRepo.RemoveCard(shared.ID, theirs.ID)).To(Equal(rest.ErrPermissionDenied))
		})

		It("returns not-found for membership writes on an unknown folder", func() {
			Expect(regularRepo.AddCards("does-not-exist", []string{mine.ID})).To(Equal(rest.ErrNotFound))
			Expect(regularRepo.RemoveCard("does-not-exist", mine.ID)).To(Equal(rest.ErrNotFound))
		})

		It("removes a card from the folder without deleting the card", func() {
			Expect(regularRepo.AddCards(deck.ID, []string{mine.ID})).To(Succeed())
			Expect(regularRepo.RemoveCard(deck.ID, mine.ID)).To(Succeed())

			got, err := regularRepo.Get(deck.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(BeEmpty())

			_, err = cardRepoAs(regularUser).Get(mine.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("lets one card live in several folders", func() {
			other := folder("Another deck")
			Expect(regularRepo.Put(other)).To(Succeed())
			Expect(regularRepo.AddCards(deck.ID, []string{mine.ID})).To(Succeed())
			Expect(regularRepo.AddCards(other.ID, []string{mine.ID})).To(Succeed())

			a, err := regularRepo.Get(deck.ID)
			Expect(err).ToNot(HaveOccurred())
			b, err := regularRepo.Get(other.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(a.CardIDs).To(ConsistOf(mine.ID))
			Expect(b.CardIDs).To(ConsistOf(mine.ID))
		})
	})

	Describe("Cards", func() {
		var shared, private *model.MusicCardFolder
		var card *model.MusicCard

		BeforeEach(func() {
			card = newCard(cardRepoAs(regularUser), "旋律")
			snippetRepo := NewMusicCardSnippetRepository(regularRepo.ctx, database).(*musicCardSnippetRepository)
			Expect(snippetRepo.Put(&model.MusicCardSnippet{
				CardID:      card.ID,
				MediaFileID: songDayInALife.ID,
				SnippetText: "line",
				Reading:     "reading",
				SongTitle:   "title",
				SongArtist:  "artist",
				FullLyrics:  "lyrics",
			})).To(Succeed())

			shared = folder("Shared deck")
			shared.Public = true
			Expect(regularRepo.Put(shared)).To(Succeed())
			Expect(regularRepo.AddCards(shared.ID, []string{card.ID})).To(Succeed())

			private = folder("Private deck")
			Expect(regularRepo.Put(private)).To(Succeed())
			Expect(regularRepo.AddCards(private.ID, []string{card.ID})).To(Succeed())
		})

		It("returns the cards with their snippets to the owner", func() {
			cards, err := regularRepo.Cards(private.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(cards).To(HaveLen(1))
			Expect(cards[0].KanjiText).To(Equal("旋律"))
			Expect(cards[0].Snippets).To(HaveLen(1))
			Expect(cards[0].Snippets[0].SnippetText).To(Equal("line"))
		})

		It("returns another user's public folder cards with their snippets", func() {
			cards, err := thirdRepo.Cards(shared.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(cards).To(HaveLen(1))
			Expect(cards[0].UserID).To(Equal(regularUser.ID))
			Expect(cards[0].Snippets).To(HaveLen(1))
		})

		It("returns not-found for a private folder of another user, admins included", func() {
			_, err := thirdRepo.Cards(private.ID)
			Expect(err).To(Equal(model.ErrNotFound))

			_, err = adminRepo.Cards(private.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("returns not-found for an unknown folder", func() {
			_, err := regularRepo.Cards("does-not-exist")
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("returns an empty list for an empty folder", func() {
			empty := folder("Empty deck")
			Expect(regularRepo.Put(empty)).To(Succeed())
			cards, err := regularRepo.Cards(empty.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(cards).To(BeEmpty())
		})
	})

	Describe("Cascade delete", func() {
		It("drops the membership when the card is deleted", func() {
			cardRepo := cardRepoAs(regularUser)
			card := newCard(cardRepo, "削除")
			deck := folder("Deck")
			Expect(regularRepo.Put(deck)).To(Succeed())
			Expect(regularRepo.AddCards(deck.ID, []string{card.ID})).To(Succeed())

			Expect(cardRepo.Delete(card.ID)).To(Succeed())

			got, err := regularRepo.Get(deck.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.CardIDs).To(BeEmpty())
		})

		It("removes folders when the owning user is deleted", func() {
			ctx := log.NewContext(context.TODO())
			ctx = request.WithUser(ctx, adminUser)
			userRepo := NewUserRepository(ctx, database)
			disposable := model.User{UserName: "folder-cascade-test-user", Name: "Cascade Test", Email: "folder-cascade@example.com"}
			Expect(userRepo.Put(&disposable)).To(Succeed())

			disposableRepo := repoAs(disposable)
			f := folder("Doomed deck")
			Expect(disposableRepo.Put(f)).To(Succeed())

			Expect(userRepo.Delete(disposable.ID)).To(Succeed())

			_, err := disposableRepo.Get(f.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})
	})

	Describe("EntityName/NewInstance", func() {
		It("returns the right entity name", func() {
			Expect(regularRepo.EntityName()).To(Equal("music_card_folder"))
		})

		It("defaults review_enabled to true on a new instance, so a payload that omits it gets review on", func() {
			Expect(regularRepo.NewInstance()).To(Equal(&model.MusicCardFolder{ReviewEnabled: true}))
		})
	})
})
