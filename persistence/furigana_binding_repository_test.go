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

var _ = Describe("FuriganaBindingRepository", func() {
	var database *dbx.DB
	var adminRepo *furiganaBindingRepository
	var regularRepo *furiganaBindingRepository
	var thirdRepo *furiganaBindingRepository

	repoAs := func(u model.User) *furiganaBindingRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewFuriganaBindingRepository(ctx, database).(*furiganaBindingRepository)
	}

	deleteAllOwnedBy := func(repo *furiganaBindingRepository) {
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

	binding := func() *model.FuriganaBinding {
		return &model.FuriganaBinding{
			MediaFileID: songDayInALife.ID,
			LineIndex:   0,
			CharOffset:  3,
			SpanLength:  2,
			KanjiText:   "漢字",
			Reading:     "かんじ",
			Display:     true,
		}
	}

	Describe("Put (upsert)", func() {
		It("creates a new binding, forcing user_id from the auth context", func() {
			b := binding()
			b.UserID = "attacker-supplied-id" // must be ignored
			Expect(regularRepo.Put(b)).To(Succeed())
			Expect(b.ID).ToNot(BeEmpty())
			Expect(b.UserID).To(Equal(regularUser.ID))

			got, err := regularRepo.Get(b.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.UserID).To(Equal(regularUser.ID))
			Expect(got.KanjiText).To(Equal("漢字"))
			Expect(got.Reading).To(Equal("かんじ"))
		})

		It("replaces the binding at the same natural key instead of erroring on the unique constraint", func() {
			b := binding()
			Expect(regularRepo.Put(b)).To(Succeed())
			firstID := b.ID

			again := binding()
			again.Reading = "updated-reading"
			Expect(regularRepo.Put(again)).To(Succeed())
			Expect(again.ID).To(Equal(firstID), "upsert must keep the same row/id")

			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1))
			Expect(all[0].Reading).To(Equal("updated-reading"))
		})

		It("lets two different users bind the same song/line/offset independently", func() {
			a := binding()
			Expect(regularRepo.Put(a)).To(Succeed())

			b := binding()
			b.Reading = "third-users-reading"
			Expect(thirdRepo.Put(b)).To(Succeed())

			Expect(a.ID).ToNot(Equal(b.ID))

			regularAll, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(regularAll).To(HaveLen(1))

			thirdAll, err := thirdRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(thirdAll).To(HaveLen(1))
			Expect(thirdAll[0].Reading).To(Equal("third-users-reading"))
		})
	})

	Describe("List by song", func() {
		It("scopes GetAll to the current user and supports filtering by media_file_id", func() {
			mine := binding()
			Expect(regularRepo.Put(mine)).To(Succeed())

			otherSong := binding()
			otherSong.MediaFileID = songComeTogether.ID
			otherSong.CharOffset = 9
			Expect(regularRepo.Put(otherSong)).To(Succeed())

			someoneElses := binding()
			Expect(thirdRepo.Put(someoneElses)).To(Succeed())

			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(2), "must not see thirdUser's binding")

			filtered, err := regularRepo.GetAll(model.QueryOptions{Filters: eqFilter("media_file_id", songDayInALife.ID)})
			Expect(err).ToNot(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].MediaFileID).To(Equal(songDayInALife.ID))
		})
	})

	Describe("Ownership enforcement", func() {
		var victim *model.FuriganaBinding

		BeforeEach(func() {
			victim = binding()
			Expect(regularRepo.Put(victim)).To(Succeed())
		})

		It("does not let another user read the binding by id", func() {
			_, err := thirdRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let another user's GetAll include the binding", func() {
			all, err := thirdRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(BeEmpty())
		})

		It("does not let another user modify the binding by id, even by spoofing user_id in the payload", func() {
			spoofed := &model.FuriganaBinding{
				ID:          victim.ID,
				UserID:      thirdUser.ID, // attacker's own id, spoofed
				MediaFileID: victim.MediaFileID,
				LineIndex:   victim.LineIndex,
				CharOffset:  victim.CharOffset,
				Reading:     "hijacked",
			}
			err := thirdRepo.Update(victim.ID, spoofed, "reading")
			Expect(err).To(Equal(rest.ErrPermissionDenied))

			stillOwned, err := regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stillOwned.Reading).To(Equal(victim.Reading))
			Expect(stillOwned.UserID).To(Equal(regularUser.ID))
		})

		It("does not let another user delete the binding by id", func() {
			err := thirdRepo.Delete(victim.ID)
			Expect(err).To(Equal(rest.ErrPermissionDenied))

			_, err = regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("lets the owner update their own binding, without reassigning ownership via payload", func() {
			reassign := &model.FuriganaBinding{
				ID:          victim.ID,
				UserID:      thirdUser.ID, // attempted give-away, must be ignored
				MediaFileID: victim.MediaFileID,
				LineIndex:   victim.LineIndex,
				CharOffset:  victim.CharOffset,
				Reading:     "renamed-by-owner",
				Display:     false,
			}
			err := regularRepo.Update(victim.ID, reassign, "reading", "display", "user_id")
			Expect(err).ToNot(HaveOccurred())

			stored, err := regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.Reading).To(Equal("renamed-by-owner"))
			Expect(stored.Display).To(BeFalse())
			Expect(stored.UserID).To(Equal(regularUser.ID))
		})

		It("lets the owner delete their own binding", func() {
			Expect(regularRepo.Delete(victim.ID)).To(Succeed())
			_, err := regularRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let an admin read or delete another user's binding", func() {
			_, err := adminRepo.Get(victim.ID)
			Expect(err).To(Equal(model.ErrNotFound), "bindings are a user's own reading choices - being an admin is not a licence to read them")

			Expect(adminRepo.Delete(victim.ID)).To(Equal(rest.ErrPermissionDenied))

			survived, err := regularRepo.Get(victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(survived.ID).To(Equal(victim.ID))
		})

		It("returns not-found when deleting a nonexistent binding", func() {
			err := regularRepo.Delete("does-not-exist")
			Expect(err).To(Equal(rest.ErrNotFound))
		})
	})

	Describe("Save via the rest.Persistable interface", func() {
		It("upserts through Save exactly like Put", func() {
			b := binding()
			id1, err := regularRepo.Save(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(id1).ToNot(BeEmpty())

			again := binding()
			again.Reading = "resaved"
			id2, err := regularRepo.Save(again)
			Expect(err).ToNot(HaveOccurred())
			Expect(id2).To(Equal(id1))
		})
	})

	Describe("Cascade delete", func() {
		It("removes bindings when the owning user is deleted", func() {
			// A disposable user (not one of the shared suite fixtures) so deleting it can't
			// affect other spec files that rely on adminUser/regularUser/thirdUser existing.
			ctx := log.NewContext(context.TODO())
			ctx = request.WithUser(ctx, adminUser)
			userRepo := NewUserRepository(ctx, database)
			disposable := model.User{UserName: "furigana-cascade-test-user", Name: "Cascade Test", Email: "cascade@example.com"}
			Expect(userRepo.Put(&disposable)).To(Succeed())

			// Read back as the owner, not as an admin: ownership scoping applies to admins too, so
			// an admin read would return not-found whether or not the cascade fired.
			disposableRepo := repoAs(disposable)
			b := binding()
			Expect(disposableRepo.Put(b)).To(Succeed())

			Expect(userRepo.Delete(disposable.ID)).To(Succeed())

			_, err := disposableRepo.Get(b.ID)
			Expect(err).To(Equal(model.ErrNotFound), "binding must be gone once its owning user is deleted")
		})
	})

	Describe("EntityName/NewInstance", func() {
		It("returns the right entity name", func() {
			Expect(regularRepo.EntityName()).To(Equal("furigana_binding"))
		})

		It("returns a new instance", func() {
			Expect(regularRepo.NewInstance()).To(Equal(&model.FuriganaBinding{}))
		})
	})
})
