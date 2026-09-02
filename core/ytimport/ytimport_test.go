package ytimport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Importer", func() {
	var (
		ds      *tests.MockDataStore
		imp     *importer
		libPath string
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		libPath = GinkgoT().TempDir()
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 1, Path: libPath}})
		ds = &tests.MockDataStore{MockedLibrary: libRepo}
		imp = New(ds).(*importer)
	})

	stubDownload := func(mp3Name string) string {
		finalPath := filepath.Join(libPath, Subfolder, mp3Name)
		imp.run = func(_ context.Context, args ...string) (string, string, error) {
			output := args[slices.Index(args, "--output")+1]
			Expect(args).To(ContainElement("--no-overwrites"))
			Expect(output).To(ContainSubstring("%(id)s"))
			mp3Path := filepath.Join(filepath.Dir(output), mp3Name)
			Expect(os.MkdirAll(filepath.Dir(mp3Path), 0o755)).To(Succeed())
			Expect(os.WriteFile(mp3Path, []byte("mp3"), 0o644)).To(Succeed())
			out := mp3Path + separator + "Song Title" + separator + "The Artist" + separator + "213.5\n"
			return out, "", nil
		}
		return finalPath
	}

	newLrclibServer := func(status int, body string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/api/get"))
			Expect(r.URL.Query().Get("track_name")).To(Equal("Song Title"))
			Expect(r.URL.Query().Get("artist_name")).To(Equal("The Artist"))
			Expect(r.URL.Query().Get("duration")).To(Equal("214"))
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		imp.lrclibBaseURL = srv.URL
		return srv
	}

	It("refuses a non-http URL", func() {
		_, err := imp.Import(ctx, "ftp://example.com/x", 1)
		var dlErr *DownloadFailedError
		Expect(err).To(BeAssignableToTypeOf(dlErr))
	})

	It("maps a missing yt-dlp binary to ErrYtdlpNotFound", func() {
		imp.run = func(_ context.Context, _ ...string) (string, string, error) {
			return "", "", &exec.Error{Name: "yt-dlp", Err: exec.ErrNotFound}
		}
		_, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).To(MatchError(ErrYtdlpNotFound))
	})

	It("surfaces the yt-dlp ERROR line on failure", func() {
		imp.run = func(_ context.Context, _ ...string) (string, string, error) {
			return "", "WARNING: something\nERROR: [youtube] video unavailable\n", &exec.ExitError{}
		}
		_, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		var dlErr *DownloadFailedError
		Expect(errors.As(err, &dlErr)).To(BeTrue())
		Expect(dlErr.Detail).To(Equal("ERROR: [youtube] video unavailable"))
		entries, readErr := os.ReadDir(filepath.Join(libPath, Subfolder))
		Expect(readErr).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("keeps videos with the same title and different ids", func() {
		firstPath := stubDownload("Same title [one].mp3")
		newLrclibServer(http.StatusNotFound, "")
		_, err := imp.Import(ctx, "https://www.youtube.com/watch?v=one", 1)
		Expect(err).ToNot(HaveOccurred())

		secondPath := stubDownload("Same title [two].mp3")
		_, err = imp.Import(ctx, "https://www.youtube.com/watch?v=two", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(firstPath).To(BeAnExistingFile())
		Expect(secondPath).To(BeAnExistingFile())
	})

	It("parses the yt-dlp print output and rounds the duration", func() {
		stubDownload("Song Title.mp3")
		newLrclibServer(http.StatusNotFound, "")
		result, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Title).To(Equal("Song Title"))
		Expect(result.Artist).To(Equal("The Artist"))
		Expect(result.Duration).To(Equal(214))
		Expect(result.LyricsFound).To(BeFalse())
	})

	It("writes a .lrc sidecar when LRCLIB has synced lyrics", func() {
		mp3Path := stubDownload("Song Title.mp3")
		srv := newLrclibServer(http.StatusOK, `{"syncedLyrics":"[00:01.00] Hello","plainLyrics":"Hello"}`)
		defer srv.Close()

		result, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.LyricsFound).To(BeTrue())
		Expect(result.LyricsSynced).To(BeTrue())

		sidecar := filepath.Join(filepath.Dir(mp3Path), "Song Title.lrc")
		content, err := os.ReadFile(sidecar)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("[00:01.00] Hello"))
	})

	It("falls back to plain lyrics and marks them unsynced", func() {
		mp3Path := stubDownload("Song Title.mp3")
		srv := newLrclibServer(http.StatusOK, `{"syncedLyrics":"","plainLyrics":"Hello there"}`)
		defer srv.Close()

		result, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.LyricsFound).To(BeTrue())
		Expect(result.LyricsSynced).To(BeFalse())
		Expect(filepath.Join(filepath.Dir(mp3Path), "Song Title.lrc")).To(BeAnExistingFile())
	})

	It("succeeds without lyrics when LRCLIB has no match", func() {
		mp3Path := stubDownload("Song Title.mp3")
		srv := newLrclibServer(http.StatusNotFound, "")
		defer srv.Close()

		result, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.LyricsFound).To(BeFalse())
		Expect(filepath.Join(filepath.Dir(mp3Path), "Song Title.lrc")).ToNot(BeAnExistingFile())
	})

	It("times out a stalled LRCLIB request", func() {
		stubDownload("Song Title.mp3")
		Expect(imp.httpClient.Timeout).To(BeNumerically(">", 0))
		imp.httpClient = &http.Client{Timeout: 20 * time.Millisecond}
		cancelled := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			close(cancelled)
		}))
		defer srv.Close()
		imp.lrclibBaseURL = srv.URL

		result, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.LyricsFound).To(BeFalse())
		Eventually(cancelled).Should(BeClosed())
	})

	It("downloads into the YouTube subfolder of the library root", func() {
		mp3Path := stubDownload("Song Title.mp3")
		newLrclibServer(http.StatusNotFound, "")
		_, err := imp.Import(ctx, "https://www.youtube.com/watch?v=x", 1)
		Expect(err).ToNot(HaveOccurred())
		Expect(mp3Path).To(HavePrefix(filepath.Join(libPath, "YouTube")))
	})
})
