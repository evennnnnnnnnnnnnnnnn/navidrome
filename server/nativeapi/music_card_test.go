package nativeapi

import (
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Music Card Clip Endpoint", func() {
	var (
		ds      *tests.MockDataStore
		mfRepo  *tests.MockMediaFileRepo
		ff      *tests.MockFFmpeg
		user    model.User
		mediaID string
	)

	BeforeEach(func() {
		mfRepo = tests.CreateMockMediaFileRepo()
		mfRepo.SetData(model.MediaFiles{
			{ID: "mf1", Path: "song.mp3", LibraryPath: "/music", Duration: 10.0},
		})
		ds = &tests.MockDataStore{MockedMediaFile: mfRepo}
		ff = tests.NewMockFFmpeg("clip-bytes")
		user = model.User{ID: "u1", UserName: "user"}
		mediaID = "mf1"
	})

	doRequest := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/musiccard/clip?"+query, nil)
		req = req.WithContext(request.WithUser(req.Context(), user))
		w := httptest.NewRecorder()
		getMusicCardClip(ds, ff)(w, req)
		return w
	}

	It("returns the clip for valid params, for any authenticated user", func() {
		w := doRequest("media_file_id=" + mediaID + "&start_ms=2000&end_ms=4000")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("Content-Type")).To(Equal("audio/mpeg"))
		Expect(w.Body.String()).To(Equal("clip-bytes"))
	})

	It("pads the requested window by 300ms on each side", func() {
		w := doRequest("media_file_id=" + mediaID + "&start_ms=2000&end_ms=4000")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(ff.LastTranscodeOptions.StartOffsetMs).To(Equal(1700))
		Expect(ff.LastTranscodeOptions.DurationMs).To(Equal(2600)) // (4300 - 1700)
	})

	It("clamps the padded window to the track's own bounds", func() {
		// track duration is 10s (10000ms); start near 0 and end near the end
		w := doRequest("media_file_id=" + mediaID + "&start_ms=100&end_ms=9900")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(ff.LastTranscodeOptions.StartOffsetMs).To(Equal(0))
		Expect(ff.LastTranscodeOptions.DurationMs).To(Equal(10000))
	})

	It("returns 400 when media_file_id is missing", func() {
		w := doRequest("start_ms=1000&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 when start_ms is not a number", func() {
		w := doRequest("media_file_id=" + mediaID + "&start_ms=abc&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 when end_ms is not a number", func() {
		w := doRequest("media_file_id=" + mediaID + "&start_ms=1000&end_ms=xyz")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 when end_ms is not after start_ms", func() {
		w := doRequest("media_file_id=" + mediaID + "&start_ms=2000&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 when start_ms is negative", func() {
		w := doRequest("media_file_id=" + mediaID + "&start_ms=-100&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 404 when the media file doesn't exist", func() {
		w := doRequest("media_file_id=does-not-exist&start_ms=1000&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 500 when the repository errors", func() {
		mfRepo.Err = true
		w := doRequest("media_file_id=" + mediaID + "&start_ms=1000&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusInternalServerError))
	})

	It("returns 500 when ffmpeg fails to extract the clip", func() {
		ff.Error = errors.New("ffmpeg exploded")
		w := doRequest("media_file_id=" + mediaID + "&start_ms=1000&end_ms=2000")
		Expect(w.Code).To(Equal(http.StatusInternalServerError))
	})
})
