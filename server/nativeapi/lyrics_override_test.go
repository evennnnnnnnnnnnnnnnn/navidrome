package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lyrics Override Endpoints", func() {
	var (
		ds   *tests.MockDataStore
		repo *tests.MockLyricsOverrideRepo
		user model.User
	)

	BeforeEach(func() {
		repo = tests.CreateMockLyricsOverrideRepo()
		user = model.User{ID: "u1", UserName: "user"}
		ds = &tests.MockDataStore{MockedLyricsOverride: repo}
	})

	withURLParam := func(req *http.Request, key, value string) *http.Request {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add(key, value)
		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	Describe("GET /lyricsoverride/{id}", func() {
		It("returns the override lyrics for any authenticated user", func() {
			list := model.LyricList{model.Lyrics{Lang: "eng", Line: []model.Line{{Value: "Override line"}}}}
			Expect(repo.Put("song1", list)).To(Succeed())

			req := httptest.NewRequest("GET", "/lyricsoverride/song1", nil)
			req = req.WithContext(request.WithUser(req.Context(), user))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			getLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
			var resp model.LyricList
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp).To(Equal(list))
		})

		It("returns 404 when no override exists", func() {
			req := httptest.NewRequest("GET", "/lyricsoverride/missing", nil)
			req = req.WithContext(request.WithUser(req.Context(), user))
			req = withURLParam(req, "id", "missing")
			w := httptest.NewRecorder()

			getLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 500 when the repository errors", func() {
			repo.Err = true
			req := httptest.NewRequest("GET", "/lyricsoverride/song1", nil)
			req = req.WithContext(request.WithUser(req.Context(), user))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			getLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("PUT /lyricsoverride/{id}", func() {
		It("saves the override for an admin user", func() {
			list := model.LyricList{model.Lyrics{Lang: "eng", Line: []model.Line{{Value: "New override"}}}}
			body, _ := json.Marshal(list)
			req := httptest.NewRequest("PUT", "/lyricsoverride/song1", bytes.NewReader(body))
			req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "admin", IsAdmin: true}))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			saveLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusNoContent))
			Expect(repo.Data).To(HaveKey("song1"))
		})

		It("returns 403 for a non-admin user", func() {
			repo.PermissionDenied = true
			list := model.LyricList{model.Lyrics{Lang: "eng", Line: []model.Line{{Value: "New override"}}}}
			body, _ := json.Marshal(list)
			req := httptest.NewRequest("PUT", "/lyricsoverride/song1", bytes.NewReader(body))
			req = req.WithContext(request.WithUser(req.Context(), user))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			saveLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusForbidden))
			Expect(repo.Data).ToNot(HaveKey("song1"))
		})

		It("returns 400 for malformed JSON", func() {
			req := httptest.NewRequest("PUT", "/lyricsoverride/song1", bytes.NewReader([]byte("not json")))
			req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "admin", IsAdmin: true}))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			saveLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("DELETE /lyricsoverride/{id}", func() {
		It("deletes the override for an admin user", func() {
			list := model.LyricList{model.Lyrics{Lang: "eng", Line: []model.Line{{Value: "To delete"}}}}
			Expect(repo.Put("song1", list)).To(Succeed())

			req := httptest.NewRequest("DELETE", "/lyricsoverride/song1", nil)
			req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "admin", IsAdmin: true}))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			deleteLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusNoContent))
			Expect(repo.Data).ToNot(HaveKey("song1"))
		})

		It("returns 403 for a non-admin user", func() {
			repo.PermissionDenied = true
			req := httptest.NewRequest("DELETE", "/lyricsoverride/song1", nil)
			req = req.WithContext(request.WithUser(req.Context(), user))
			req = withURLParam(req, "id", "song1")
			w := httptest.NewRecorder()

			deleteLyricsOverride(ds)(w, req)
			Expect(w.Code).To(Equal(http.StatusForbidden))
		})
	})
})
