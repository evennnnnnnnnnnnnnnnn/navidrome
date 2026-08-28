package nativeapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// permissionDeniedReviewRepo simulates the repository refusing a write because the card belongs
// to another user - the persistence tests prove the real repository does this.
type permissionDeniedReviewRepo struct {
	*tests.MockMusicCardReviewRepo
}

func (permissionDeniedReviewRepo) Put(*model.MusicCardReview) error {
	return rest.ErrPermissionDenied
}

var _ = Describe("Music Card Review Grade Endpoint", func() {
	var (
		ds         *tests.MockDataStore
		cardRepo   *tests.MockMusicCardRepo
		reviewRepo *tests.MockMusicCardReviewRepo
		user       model.User
	)

	BeforeEach(func() {
		cardRepo = tests.CreateMockMusicCardRepo()
		Expect(cardRepo.Put(&model.MusicCard{ID: "card-1", UserID: "u1", KanjiText: "漢字"})).To(Succeed())
		reviewRepo = tests.CreateMockMusicCardReviewRepo()
		ds = &tests.MockDataStore{MockedMusicCard: cardRepo, MockedMusicCardReview: reviewRepo}
		user = model.User{ID: "u1", UserName: "user"}
	})

	doRequest := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/musiccardreview/grade", strings.NewReader(body))
		req = req.WithContext(request.WithUser(req.Context(), user))
		w := httptest.NewRecorder()
		postMusicCardReviewGrade(ds)(w, req)
		return w
	}

	It("creates review state with SM-2 defaults on the first grade of a card", func() {
		w := doRequest(`{"card_id":"card-1","grade":"good"}`)
		Expect(w.Code).To(Equal(http.StatusOK))

		var rev model.MusicCardReview
		Expect(json.Unmarshal(w.Body.Bytes(), &rev)).To(Succeed())
		Expect(rev.CardID).To(Equal("card-1"))
		Expect(rev.RepetitionCount).To(Equal(1))
		Expect(rev.IntervalDays).To(Equal(1.0))
		Expect(rev.EaseFactor).To(Equal(model.ReviewDefaultEase))

		stored, err := reviewRepo.GetByCardID("card-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.RepetitionCount).To(Equal(1))
	})

	It("applies the transition to existing review state", func() {
		existing := model.NewMusicCardReview("card-1", time.Now())
		Expect(existing.ApplyGrade(model.ReviewGradeGood, time.Now())).To(Succeed())
		Expect(reviewRepo.Put(existing)).To(Succeed())

		w := doRequest(`{"card_id":"card-1","grade":"good"}`)
		Expect(w.Code).To(Equal(http.StatusOK))

		var rev model.MusicCardReview
		Expect(json.Unmarshal(w.Body.Bytes(), &rev)).To(Succeed())
		Expect(rev.RepetitionCount).To(Equal(2))
		Expect(rev.IntervalDays).To(Equal(6.0))
	})

	It("returns 400 for a malformed body", func() {
		w := doRequest(`{not json`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 when card_id is missing", func() {
		w := doRequest(`{"grade":"good"}`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 for an unknown grade", func() {
		w := doRequest(`{"card_id":"card-1","grade":"perfect"}`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 404 when the card does not exist", func() {
		w := doRequest(`{"card_id":"no-such-card","grade":"good"}`)
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 404 when the repository refuses the write for a card owned by someone else", func() {
		ds.MockedMusicCardReview = permissionDeniedReviewRepo{reviewRepo}
		w := doRequest(`{"card_id":"card-1","grade":"good"}`)
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 500 when the review repository errors", func() {
		reviewRepo.SetError(true)
		w := doRequest(`{"card_id":"card-1","grade":"good"}`)
		Expect(w.Code).To(Equal(http.StatusInternalServerError))
	})
})
