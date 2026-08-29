package persistence

import (
	"context"
	"time"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pocketbase/dbx"
)

var _ = Describe("MusicCardReviewRepository", func() {
	var database *dbx.DB
	var adminRepo *musicCardReviewRepository
	var regularRepo *musicCardReviewRepository
	var thirdRepo *musicCardReviewRepository
	var regularCardRepo *musicCardRepository
	var thirdCardRepo *musicCardRepository
	var regularCard *model.MusicCard
	var thirdCard *model.MusicCard

	repoAs := func(u model.User) *musicCardReviewRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardReviewRepository(ctx, database).(*musicCardReviewRepository)
	}

	cardRepoAs := func(u model.User) *musicCardRepository {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, u)
		return NewMusicCardRepository(ctx, database).(*musicCardRepository)
	}

	deleteAllCardsOwnedBy := func(repo *musicCardRepository) {
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
		regularCardRepo = cardRepoAs(regularUser)
		thirdCardRepo = cardRepoAs(thirdUser)

		regularCard = &model.MusicCard{KanjiText: "漢字"}
		Expect(regularCardRepo.Put(regularCard)).To(Succeed())
		thirdCard = &model.MusicCard{KanjiText: "漢字"}
		Expect(thirdCardRepo.Put(thirdCard)).To(Succeed())
	})

	AfterEach(func() {
		// Deleting the cards cascades to their review rows.
		deleteAllCardsOwnedBy(regularCardRepo)
		deleteAllCardsOwnedBy(thirdCardRepo)
	})

	review := func(cardID string, dueAt time.Time) *model.MusicCardReview {
		rev := model.NewMusicCardReview(cardID, dueAt)
		rev.DueAt = dueAt
		return rev
	}

	Describe("Put (upsert by card_id)", func() {
		It("creates a review row for an owned card", func() {
			rev := review(regularCard.ID, time.Now())
			Expect(regularRepo.Put(rev)).To(Succeed())
			Expect(rev.ID).ToNot(BeEmpty())

			got, err := regularRepo.GetByCardID(regularCard.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(rev.ID))
			Expect(got.EaseFactor).To(Equal(model.ReviewDefaultEase))
		})

		It("updates in place instead of duplicating when the card already has review state", func() {
			first := review(regularCard.ID, time.Now())
			Expect(regularRepo.Put(first)).To(Succeed())

			second := review(regularCard.ID, time.Now())
			second.RepetitionCount = 3
			Expect(regularRepo.Put(second)).To(Succeed())
			Expect(second.ID).To(Equal(first.ID), "upsert must keep the same row/id")

			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1))
			Expect(all[0].RepetitionCount).To(Equal(3))
		})

		It("does not let a user attach review state to another user's card", func() {
			rev := review(thirdCard.ID, time.Now())
			Expect(regularRepo.Put(rev)).To(Equal(rest.ErrPermissionDenied))
		})

		It("returns not-found for a card that does not exist", func() {
			rev := review("no-such-card", time.Now())
			Expect(regularRepo.Put(rev)).To(Equal(model.ErrNotFound))
		})

		It("does not let an admin write review state on another user's card", func() {
			rev := review(regularCard.ID, time.Now())
			Expect(adminRepo.Put(rev)).To(Equal(rest.ErrPermissionDenied))
		})
	})

	Describe("Ownership scoping on reads", func() {
		var mine *model.MusicCardReview
		var theirs *model.MusicCardReview

		BeforeEach(func() {
			mine = review(regularCard.ID, time.Now())
			Expect(regularRepo.Put(mine)).To(Succeed())
			theirs = review(thirdCard.ID, time.Now())
			Expect(thirdRepo.Put(theirs)).To(Succeed())
		})

		It("scopes GetAll to reviews on the caller's own cards", func() {
			all, err := regularRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1))
			Expect(all[0].CardID).To(Equal(regularCard.ID))
		})

		It("does not let another user read a review by id", func() {
			_, err := thirdRepo.Get(mine.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let another user read a review by card id", func() {
			_, err := thirdRepo.GetByCardID(regularCard.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})

		It("does not let an admin see other users' review state", func() {
			all, err := adminRepo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(BeEmpty(), "the admin owns no cards, so it must see no review state")

			_, err = adminRepo.Get(mine.ID)
			Expect(err).To(Equal(model.ErrNotFound))
		})
	})

	Describe("due_before filter", func() {
		// Both cards belong to regularUser: ownership scoping applies to admins too, so the filter
		// has to be exercised from inside a single user's own queue.
		BeforeEach(func() {
			laterCard := &model.MusicCard{KanjiText: "音楽"}
			Expect(regularCardRepo.Put(laterCard)).To(Succeed())

			// Same-day timestamps on purpose: stored values carry the local utc-offset while the
			// filter value is a UTC "Z" string, so this catches any lexical (non-normalized)
			// datetime comparison.
			overdue := review(regularCard.ID, time.Now().Add(-10*time.Minute))
			Expect(regularRepo.Put(overdue)).To(Succeed())

			future := review(laterCard.ID, time.Now().Add(10*time.Minute))
			Expect(regularRepo.Put(future)).To(Succeed())
		})

		It("returns only rows due at or before the given moment", func() {
			due, err := regularRepo.GetAll(model.QueryOptions{
				Filters: dueBeforeFilter("due_before", time.Now().UTC().Format(time.RFC3339)),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(due).To(HaveLen(1))
			Expect(due[0].CardID).To(Equal(regularCard.ID))
		})

		It("matches nothing for an unparseable value", func() {
			due, err := regularRepo.GetAll(model.QueryOptions{
				Filters: dueBeforeFilter("due_before", "not-a-time"),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(due).To(BeEmpty())
		})
	})

	Describe("Cascade delete", func() {
		It("removes the review row when its card is deleted", func() {
			rev := review(regularCard.ID, time.Now())
			Expect(regularRepo.Put(rev)).To(Succeed())

			Expect(regularCardRepo.Delete(regularCard.ID)).To(Succeed())

			_, err := regularRepo.Get(rev.ID)
			Expect(err).To(Equal(model.ErrNotFound), "review state must be gone once its card is deleted (FK cascade)")
		})
	})

	Describe("EntityName/NewInstance", func() {
		It("returns the right entity name", func() {
			Expect(regularRepo.EntityName()).To(Equal("music_card_review"))
		})

		It("returns a new instance", func() {
			Expect(regularRepo.NewInstance()).To(Equal(&model.MusicCardReview{}))
		})
	})
})
