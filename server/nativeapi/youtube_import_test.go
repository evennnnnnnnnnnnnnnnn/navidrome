package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/ytimport"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeYtImporter struct {
	result    *ytimport.Result
	err       error
	gotURL    string
	gotLibID  int
	wasCalled bool
}

func (f *fakeYtImporter) Import(_ context.Context, rawURL string, libraryID int) (*ytimport.Result, error) {
	f.wasCalled = true
	f.gotURL = rawURL
	f.gotLibID = libraryID
	return f.result, f.err
}

var _ = Describe("YouTube Import Endpoint", func() {
	var (
		api  *Router
		fake *fakeYtImporter
	)

	adminUser := model.User{ID: "a1", UserName: "admin", IsAdmin: true}

	BeforeEach(func() {
		api = &Router{ds: &tests.MockDataStore{}}
		fake = &fakeYtImporter{}
		newYtImporter = func(model.DataStore) ytimport.Importer { return fake }
		callScan = func(context.Context, model.DataStore, playlists.Playlists, bool, []model.ScanTarget) (<-chan *scanner.ProgressInfo, error) {
			done := make(chan *scanner.ProgressInfo)
			close(done)
			return done, nil
		}
		DeferCleanup(func() {
			newYtImporter = ytimport.New
			callScan = scanner.CallScan
		})
	})

	doRequest := func(user model.User, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/youtubeimport", strings.NewReader(body))
		req = req.WithContext(request.WithUser(req.Context(), user))
		w := httptest.NewRecorder()
		r := chi.NewRouter()
		r.With(adminOnlyMiddleware).Group(func(r chi.Router) {
			api.addYoutubeImportRoute(r)
		})
		r.ServeHTTP(w, req)
		return w
	}

	It("refuses a non-admin user with 403 before importing", func() {
		w := doRequest(model.User{ID: "u1", UserName: "user"}, `{"url":"https://youtube.com/watch?v=x"}`)
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(fake.wasCalled).To(BeFalse())
	})

	It("refuses a missing url with 400", func() {
		w := doRequest(adminUser, `{}`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(fake.wasCalled).To(BeFalse())
	})

	It("refuses a malformed body with 400", func() {
		w := doRequest(adminUser, `not json`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("maps a missing yt-dlp binary to 503", func() {
		fake.err = ytimport.ErrYtdlpNotFound
		w := doRequest(adminUser, `{"url":"https://youtube.com/watch?v=x"}`)
		Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(w.Body.String()).To(ContainSubstring("yt-dlp was not found"))
	})

	It("maps a failed download to 422 with the yt-dlp detail", func() {
		fake.err = &ytimport.DownloadFailedError{Detail: "ERROR: video unavailable"}
		w := doRequest(adminUser, `{"url":"https://youtube.com/watch?v=x"}`)
		Expect(w.Code).To(Equal(http.StatusUnprocessableEntity))
		Expect(w.Body.String()).To(ContainSubstring("video unavailable"))
	})

	It("defaults the library to the default library id", func() {
		fake.err = &ytimport.DownloadFailedError{Detail: "stop early"}
		doRequest(adminUser, `{"url":"https://youtube.com/watch?v=x"}`)
		Expect(fake.gotLibID).To(Equal(model.DefaultLibraryID))
	})

	It("returns the import result as JSON on success", func() {
		fake.result = &ytimport.Result{
			Path: "/music/YouTube/Song.mp3", Title: "Song", Artist: "Artist",
			Duration: 214, LyricsFound: true, LyricsSynced: true,
		}
		body, _ := json.Marshal(map[string]any{"url": "https://youtube.com/watch?v=x", "libraryId": 1})
		w := doRequest(adminUser, string(bytes.TrimSpace(body)))
		Expect(w.Code).To(Equal(http.StatusOK))

		var resp youtubeImportResponse
		Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Title).To(Equal("Song"))
		Expect(resp.LyricsSynced).To(BeTrue())
		Expect(fake.gotURL).To(Equal("https://youtube.com/watch?v=x"))
		Expect(fake.gotLibID).To(Equal(1))
	})
})
