package persistence

import (
	"context"
	"encoding/json"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LyricsOverrideRepository", func() {
	var repo model.LyricsOverrideRepository

	overrideLyrics := model.LyricList{
		model.Lyrics{
			Lang: "eng",
			Line: []model.Line{
				{Value: "Admin-edited lyrics"},
			},
		},
	}

	Describe("Admin User", func() {
		BeforeEach(func() {
			ctx := log.NewContext(context.TODO())
			ctx = request.WithUser(ctx, model.User{ID: "userid", UserName: "userid", IsAdmin: true})
			repo = NewLyricsOverrideRepository(ctx, GetDBXBuilder())
		})

		AfterEach(func() {
			_ = repo.Delete(songDayInALife.ID)
		})

		Describe("Put", func() {
			It("creates a new override", func() {
				err := repo.Put(songDayInALife.ID, overrideLyrics)
				Expect(err).ToNot(HaveOccurred())

				res, err := repo.Get(songDayInALife.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.MediaFileID).To(Equal(songDayInALife.ID))
				Expect(res.UpdatedBy).To(Equal("userid"))

				list, err := res.StructuredLyrics()
				Expect(err).ToNot(HaveOccurred())
				Expect(list).To(Equal(overrideLyrics))
			})

			It("replaces an existing override", func() {
				Expect(repo.Put(songDayInALife.ID, overrideLyrics)).To(Succeed())

				updated := model.LyricList{model.Lyrics{Lang: "eng", Line: []model.Line{{Value: "Updated lyrics"}}}}
				Expect(repo.Put(songDayInALife.ID, updated)).To(Succeed())

				res, err := repo.Get(songDayInALife.ID)
				Expect(err).ToNot(HaveOccurred())
				list, err := res.StructuredLyrics()
				Expect(err).ToNot(HaveOccurred())
				Expect(list).To(Equal(updated))
			})
		})

		Describe("Get", func() {
			It("returns ErrNotFound when no override exists", func() {
				_, err := repo.Get("nonexistent-id")
				Expect(err).To(MatchError(model.ErrNotFound))
			})
		})

		Describe("Delete", func() {
			It("removes an existing override", func() {
				Expect(repo.Put(songDayInALife.ID, overrideLyrics)).To(Succeed())
				Expect(repo.Delete(songDayInALife.ID)).To(Succeed())

				_, err := repo.Get(songDayInALife.ID)
				Expect(err).To(MatchError(model.ErrNotFound))
			})
		})
	})

	Describe("Regular User", func() {
		BeforeEach(func() {
			adminCtx := log.NewContext(context.TODO())
			adminCtx = request.WithUser(adminCtx, model.User{ID: "userid", UserName: "userid", IsAdmin: true})
			adminRepo := NewLyricsOverrideRepository(adminCtx, GetDBXBuilder())
			Expect(adminRepo.Put(songDayInALife.ID, overrideLyrics)).To(Succeed())

			ctx := log.NewContext(context.TODO())
			ctx = request.WithUser(ctx, model.User{ID: "2222", UserName: "regular-user", IsAdmin: false})
			repo = NewLyricsOverrideRepository(ctx, GetDBXBuilder())
		})

		AfterEach(func() {
			adminCtx := log.NewContext(context.TODO())
			adminCtx = request.WithUser(adminCtx, model.User{ID: "userid", UserName: "userid", IsAdmin: true})
			adminRepo := NewLyricsOverrideRepository(adminCtx, GetDBXBuilder())
			_ = adminRepo.Delete(songDayInALife.ID)
		})

		It("allows reads", func() {
			res, err := repo.Get(songDayInALife.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.MediaFileID).To(Equal(songDayInALife.ID))
		})

		It("refuses to save an override", func() {
			err := repo.Put(songDayInALife.ID, overrideLyrics)
			Expect(err).To(MatchError(rest.ErrPermissionDenied))
		})

		It("refuses to delete an override", func() {
			err := repo.Delete(songDayInALife.ID)
			Expect(err).To(MatchError(rest.ErrPermissionDenied))

			// The override must still be there, untouched by the refused delete.
			adminCtx := log.NewContext(context.TODO())
			adminCtx = request.WithUser(adminCtx, model.User{ID: "userid", UserName: "userid", IsAdmin: true})
			adminRepo := NewLyricsOverrideRepository(adminCtx, GetDBXBuilder())
			_, err = adminRepo.Get(songDayInALife.ID)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("JSON round-trip", func() {
		It("stores the LyricList shape unchanged, matching buildLyricsList's input", func() {
			ctx := log.NewContext(context.TODO())
			ctx = request.WithUser(ctx, model.User{ID: "userid", UserName: "userid", IsAdmin: true})
			repo = NewLyricsOverrideRepository(ctx, GetDBXBuilder())
			defer func() { _ = repo.Delete(songDayInALife.ID) }()

			synced := model.LyricList{
				model.Lyrics{
					Kind:   model.LyricKindMain,
					Lang:   "eng",
					Synced: true,
					Line: []model.Line{
						{Start: new(int64(1000)), Value: "Line one", Cue: []model.Cue{
							{Start: new(int64(1000)), End: new(int64(1500)), Value: "Line ", ByteStart: 0, ByteEnd: 5},
						}},
					},
				},
			}
			Expect(repo.Put(songDayInALife.ID, synced)).To(Succeed())

			res, err := repo.Get(songDayInALife.ID)
			Expect(err).ToNot(HaveOccurred())

			// Confirm the raw column really is the same JSON shape produced by
			// json.Marshal(model.LyricList), i.e. what buildLyricsList expects.
			expectedJSON, _ := json.Marshal(synced)
			Expect(res.Lyrics).To(MatchJSON(expectedJSON))
		})
	})
})
