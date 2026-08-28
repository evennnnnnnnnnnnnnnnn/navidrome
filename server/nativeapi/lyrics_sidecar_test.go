package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lyrics Sidecar Endpoint", func() {
	var (
		ds      *tests.MockDataStore
		repo    *tests.MockMediaFileRepo
		user    model.User
		libRoot string
	)

	const syncedLRC = "[00:10.03] さよならの前にキスをして\n[00:15.74] さよならの後は忘れさせて\n"

	BeforeEach(func() {
		libRoot = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(libRoot, "YouTube"), 0o755)).To(Succeed())

		repo = tests.CreateMockMediaFileRepo()
		repo.SetData(model.MediaFiles{
			{
				ID:          "song1",
				LibraryPath: libRoot,
				Path:        filepath.Join("YouTube", "でも、.mp3"),
				Title:       "でも、",
			},
		})
		user = model.User{ID: "u1", UserName: "admin", IsAdmin: true}
		ds = &tests.MockDataStore{MockedMediaFile: repo}
	})

	post := func(id string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())

		req := httptest.NewRequest("POST", "/lyricssidecar/"+id, bytes.NewReader(encoded))
		req = req.WithContext(request.WithUser(req.Context(), user))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		saveLyricsSidecar(ds)(w, req)
		return w
	}

	sidecar := func() string { return filepath.Join(libRoot, "YouTube", "でも、.lrc") }

	It("writes the sidecar next to the audio file", func() {
		w := post("song1", lyricsSidecarRequest{Content: syncedLRC})
		Expect(w.Code).To(Equal(http.StatusNoContent))

		written, err := os.ReadFile(sidecar())
		Expect(err).ToNot(HaveOccurred())
		Expect(string(written)).To(Equal(syncedLRC))
	})

	It("replaces an existing sidecar", func() {
		Expect(os.WriteFile(sidecar(), []byte("[00:01.00] stale\n"), 0o644)).To(Succeed())

		w := post("song1", lyricsSidecarRequest{Content: syncedLRC})
		Expect(w.Code).To(Equal(http.StatusNoContent))

		written, err := os.ReadFile(sidecar())
		Expect(err).ToNot(HaveOccurred())
		Expect(string(written)).To(Equal(syncedLRC))
	})

	It("leaves no temp files behind", func() {
		Expect(post("song1", lyricsSidecarRequest{Content: syncedLRC}).Code).
			To(Equal(http.StatusNoContent))

		entries, err := os.ReadDir(filepath.Join(libRoot, "YouTube"))
		Expect(err).ToNot(HaveOccurred())
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		Expect(names).To(ConsistOf("でも、.lrc"))
	})

	It("accepts unsynchronized plain text", func() {
		w := post("song1", lyricsSidecarRequest{Content: "just a plain line\nand another\n"})
		Expect(w.Code).To(Equal(http.StatusNoContent))
		Expect(sidecar()).To(BeAnExistingFile())
	})

	It("refuses content with no lyric lines and writes nothing", func() {
		w := post("song1", lyricsSidecarRequest{Content: "   \n  \n"})
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(sidecar()).ToNot(BeAnExistingFile())
	})

	It("refuses an empty body and writes nothing", func() {
		w := post("song1", lyricsSidecarRequest{Content: ""})
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(sidecar()).ToNot(BeAnExistingFile())
	})

	It("returns 404 for an unknown song", func() {
		w := post("nope", lyricsSidecarRequest{Content: syncedLRC})
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	Describe("sidecarPathFor", func() {
		It("swaps the extension and stays inside the library", func() {
			path, err := sidecarPathFor(&model.MediaFile{
				LibraryPath: "/music",
				Path:        "YouTube/でも、.mp3",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/music/YouTube/でも、.lrc"))
		})

		It("refuses a stored path that escapes the library root", func() {
			_, err := sidecarPathFor(&model.MediaFile{
				LibraryPath: "/music",
				Path:        "../../etc/passwd.mp3",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("escapes the library"))
		})

		It("refuses a media file with no library path", func() {
			_, err := sidecarPathFor(&model.MediaFile{Path: "song.mp3"})
			Expect(err).To(HaveOccurred())
		})
	})
})
