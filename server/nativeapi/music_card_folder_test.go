package nativeapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Music Card Folder Cards Endpoints", func() {
	var (
		ds     *tests.MockDataStore
		repo   *tests.MockMusicCardFolderRepo
		router chi.Router
		user   model.User
	)

	BeforeEach(func() {
		repo = tests.CreateMockMusicCardFolderRepo()
		repo.Data["f1"] = &model.MusicCardFolder{ID: "f1", UserID: "u1", Name: "Shared deck", Public: true}
		repo.CardsData["f1"] = model.MusicCardsWithSnippets{
			{
				MusicCard: model.MusicCard{ID: "c1", UserID: "u1", KanjiText: "漢字"},
				Snippets:  model.MusicCardSnippets{{ID: "s1", CardID: "c1", SnippetText: "line"}},
			},
		}
		ds = &tests.MockDataStore{MockedMusicCardFolder: repo}
		user = model.User{ID: "u1", UserName: "user"}

		router = chi.NewRouter()
		api := &Router{ds: ds}
		// registered together, as in routes(): the generic resource and the cards sub-routes must
		// coexist on the same path prefix
		api.addMusicCardFolderRoute(router)
		api.addMusicCardFolderCardsRoute(router)
	})

	doRequest := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(request.WithUser(req.Context(), user))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	Describe("GET /musiccardfolder/{id}/cards", func() {
		It("returns the folder's cards with their snippets", func() {
			w := doRequest("GET", "/musiccardfolder/f1/cards", "")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))

			var got []map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			Expect(got).To(HaveLen(1))
			Expect(got[0]["id"]).To(Equal("c1"))
			Expect(got[0]["kanji_text"]).To(Equal("漢字"))
			Expect(got[0]["snippets"]).To(HaveLen(1))
		})

		It("returns 404 for a folder the caller cannot see", func() {
			w := doRequest("GET", "/musiccardfolder/unknown/cards", "")
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /musiccardfolder/{id}/cards", func() {
		It("adds the cards and returns an empty object", func() {
			w := doRequest("POST", "/musiccardfolder/f1/cards", `{"card_ids":["c1","c2"]}`)
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(Equal("{}"))
			Expect(repo.Added["f1"]).To(ConsistOf("c1", "c2"))
		})

		It("returns 400 on a malformed body", func() {
			w := doRequest("POST", "/musiccardfolder/f1/cards", `not json`)
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 404 for an unknown folder", func() {
			w := doRequest("POST", "/musiccardfolder/unknown/cards", `{"card_ids":["c1"]}`)
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 404, not 403, when the caller may not write the folder", func() {
			repo.Denied["f1"] = true
			w := doRequest("POST", "/musiccardfolder/f1/cards", `{"card_ids":["c1"]}`)
			Expect(w.Code).To(Equal(http.StatusNotFound), "permission denied must not leak the folder's existence")
		})
	})

	Describe("DELETE /musiccardfolder/{id}/cards/{cardId}", func() {
		It("removes the card and returns an empty object", func() {
			Expect(repo.AddCards("f1", []string{"c1", "c2"})).To(Succeed())

			w := doRequest("DELETE", "/musiccardfolder/f1/cards/c1", "")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(Equal("{}"))
			Expect(repo.Added["f1"]).To(ConsistOf("c2"))
		})

		It("returns 404 for an unknown folder", func() {
			w := doRequest("DELETE", "/musiccardfolder/unknown/cards/c1", "")
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 404, not 403, when the caller may not write the folder", func() {
			repo.Denied["f1"] = true
			w := doRequest("DELETE", "/musiccardfolder/f1/cards/c1", "")
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("the generic /musiccardfolder resource", func() {
		It("still lists and creates folders alongside the cards routes", func() {
			w := doRequest("GET", "/musiccardfolder", "")
			Expect(w.Code).To(Equal(http.StatusOK))

			w = doRequest("POST", "/musiccardfolder", `{"name":"JLPT N3"}`)
			Expect(w.Code).To(Equal(http.StatusOK))
			var created map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &created)).To(Succeed())
			Expect(created["id"]).ToNot(BeEmpty())
			Expect(repo.Data[created["id"]].ReviewEnabled).To(BeTrue(), "review_enabled defaults to true when the payload omits it")
		})

		It("returns 400 with a validation body when deleting the default folder", func() {
			repo.Data["def"] = &model.MusicCardFolder{ID: "def", Name: "Default", IsDefault: true}
			w := doRequest("DELETE", "/musiccardfolder/def", "")
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring("the default folder cannot be deleted"))
		})
	})
})
