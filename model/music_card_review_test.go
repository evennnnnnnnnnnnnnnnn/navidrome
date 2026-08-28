package model_test

import (
	"time"

	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MusicCardReview", func() {
	var now time.Time

	BeforeEach(func() {
		now = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	})

	newReview := func() *model.MusicCardReview {
		return model.NewMusicCardReview("card-1", now)
	}

	days := func(d float64) time.Duration {
		return time.Duration(d * 24 * float64(time.Hour))
	}

	Describe("NewMusicCardReview", func() {
		It("starts due immediately with the SM-2 default ease and no history", func() {
			r := newReview()
			Expect(r.CardID).To(Equal("card-1"))
			Expect(r.DueAt).To(Equal(now))
			Expect(r.EaseFactor).To(Equal(2.5))
			Expect(r.IntervalDays).To(BeZero())
			Expect(r.RepetitionCount).To(BeZero())
			Expect(r.LapseCount).To(BeZero())
		})
	})

	Describe("ParseReviewGrade", func() {
		It("accepts the four grades", func() {
			for _, s := range []string{"again", "hard", "good", "easy"} {
				g, err := model.ParseReviewGrade(s)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(g)).To(Equal(s))
			}
		})

		It("rejects anything else", func() {
			_, err := model.ParseReviewGrade("perfect")
			Expect(err).To(MatchError(model.ErrValidation))
		})
	})

	Describe("ApplyGrade", func() {
		It("rejects an invalid grade without touching the state", func() {
			r := newReview()
			Expect(r.ApplyGrade("perfect", now)).To(MatchError(model.ErrValidation))
			Expect(r.RepetitionCount).To(BeZero())
			Expect(r.LastReviewedAt.IsZero()).To(BeTrue())
		})

		It("stamps LastReviewedAt on every successful grade", func() {
			r := newReview()
			Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
			Expect(r.LastReviewedAt).To(Equal(now))
		})

		Describe("good", func() {
			It("progresses 1d, 6d, then ease-multiplied", func() {
				r := newReview()

				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(1))
				Expect(r.IntervalDays).To(Equal(1.0))
				Expect(r.DueAt).To(Equal(now.Add(days(1))))

				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(2))
				Expect(r.IntervalDays).To(Equal(6.0))
				Expect(r.DueAt).To(Equal(now.Add(days(6))))

				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(3))
				Expect(r.IntervalDays).To(Equal(15.0)) // 6 * 2.5
				Expect(r.DueAt).To(Equal(now.Add(days(15))))
			})

			It("does not change the ease factor", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.EaseFactor).To(Equal(2.5))
			})
		})

		Describe("again", func() {
			It("resets the repetition streak, counts a lapse, and re-queues after a short delay", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())

				Expect(r.ApplyGrade(model.ReviewGradeAgain, now)).To(Succeed())
				Expect(r.RepetitionCount).To(BeZero())
				Expect(r.LapseCount).To(Equal(1))
				Expect(r.IntervalDays).To(BeZero())
				Expect(r.EaseFactor).To(Equal(2.3))
				Expect(r.DueAt).To(Equal(now.Add(10 * time.Minute)))
			})

			It("does not count a lapse on a card that was never successfully reviewed", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeAgain, now)).To(Succeed())
				Expect(r.LapseCount).To(BeZero())
				Expect(r.RepetitionCount).To(BeZero())
			})

			It("restarts the interval progression from 1d after a lapse", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.ApplyGrade(model.ReviewGradeAgain, now)).To(Succeed())

				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(1))
				Expect(r.IntervalDays).To(Equal(1.0))
			})

			It("never drops the ease factor below the 1.3 floor", func() {
				r := newReview()
				for i := 0; i < 10; i++ {
					Expect(r.ApplyGrade(model.ReviewGradeAgain, now)).To(Succeed())
				}
				Expect(r.EaseFactor).To(Equal(1.3))
			})
		})

		Describe("hard", func() {
			It("penalises ease and grows the interval by 1.2x instead of the ease factor", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeHard, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(1))
				Expect(r.IntervalDays).To(Equal(1.0))
				Expect(r.EaseFactor).To(BeNumerically("~", 2.35, 1e-9))

				Expect(r.ApplyGrade(model.ReviewGradeHard, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(2))
				Expect(r.IntervalDays).To(BeNumerically("~", 1.2, 1e-9))
				Expect(r.EaseFactor).To(BeNumerically("~", 2.2, 1e-9))
			})

			It("never grows slower than a 1-day interval", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeAgain, now)).To(Succeed()) // interval 0
				Expect(r.ApplyGrade(model.ReviewGradeHard, now)).To(Succeed())
				Expect(r.IntervalDays).To(Equal(1.0))
			})
		})

		Describe("easy", func() {
			It("boosts ease and applies the easy bonus to the interval", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeEasy, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(1))
				Expect(r.IntervalDays).To(Equal(4.0))
				Expect(r.EaseFactor).To(BeNumerically("~", 2.65, 1e-9))
				Expect(r.DueAt).To(Equal(now.Add(days(4))))

				Expect(r.ApplyGrade(model.ReviewGradeEasy, now)).To(Succeed())
				Expect(r.RepetitionCount).To(Equal(2))
				Expect(r.IntervalDays).To(BeNumerically("~", 7.8, 1e-9)) // 6 * 1.3
				Expect(r.EaseFactor).To(BeNumerically("~", 2.8, 1e-9))
			})

			It("multiplies by ease and the easy bonus past the second repetition", func() {
				r := newReview()
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.ApplyGrade(model.ReviewGradeGood, now)).To(Succeed())
				Expect(r.ApplyGrade(model.ReviewGradeEasy, now)).To(Succeed())
				// 6 * 2.65 * 1.3 = 20.67
				Expect(r.IntervalDays).To(BeNumerically("~", 20.67, 1e-9))
			})
		})
	})
})
