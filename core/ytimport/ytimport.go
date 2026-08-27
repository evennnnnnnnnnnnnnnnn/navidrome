package ytimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Subfolder of the library root that imported audio lands in, so provenance
// stays visible in the folder structure.
const Subfolder = "YouTube"

// Field separator for the yt-dlp --print template (ASCII unit separator,
// which cannot appear in video titles or file paths).
const separator = "\x1f"

const lrclibDefaultBaseURL = "https://lrclib.net"

// LRCLIB asks clients to identify themselves; see lrclib.net/docs
const lrclibUserAgent = "Navidrome-ytimport (https://github.com/navidrome/navidrome)"

// ErrYtdlpNotFound means the yt-dlp binary is not installed or not on PATH.
var ErrYtdlpNotFound = errors.New("yt-dlp was not found. Please install it (and FFmpeg) and make sure it is on the server's PATH")

// DownloadFailedError wraps a yt-dlp run that started but did not succeed
// (bad URL, unavailable video, network failure). Detail is safe to show to
// the client.
type DownloadFailedError struct {
	Detail string
}

func (e *DownloadFailedError) Error() string {
	return fmt.Sprintf("yt-dlp failed: %s", e.Detail)
}

type Result struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Duration     int    `json:"duration"`
	LyricsFound  bool   `json:"lyricsFound"`
	LyricsSynced bool   `json:"lyricsSynced"`
}

type Importer interface {
	// Import downloads the audio of the video at rawURL as an MP3 into the
	// library's YouTube/ subfolder, then fetches synced lyrics from LRCLIB
	// into a .lrc sidecar next to it. A lyrics miss is not an error.
	Import(ctx context.Context, rawURL string, libraryID int) (*Result, error)
}

func New(ds model.DataStore) Importer {
	return &importer{
		ds:            ds,
		run:           runYtdlp,
		httpClient:    http.DefaultClient,
		lrclibBaseURL: lrclibDefaultBaseURL,
	}
}

type importer struct {
	ds            model.DataStore
	run           func(ctx context.Context, args ...string) (stdout string, stderr string, err error)
	httpClient    *http.Client
	lrclibBaseURL string
}

func (i *importer) Import(ctx context.Context, rawURL string, libraryID int) (*Result, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, &DownloadFailedError{Detail: "not a valid http(s) URL"}
	}

	libPath, err := i.ds.Library(ctx).GetPath(libraryID)
	if err != nil {
		return nil, fmt.Errorf("resolving library %d: %w", libraryID, err)
	}

	destination := filepath.Join(libPath, Subfolder)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return nil, fmt.Errorf("creating destination folder: %w", err)
	}

	result, err := i.downloadAudio(ctx, rawURL, destination)
	if err != nil {
		return nil, err
	}

	i.fetchLyrics(ctx, result)
	return result, nil
}

// downloadAudio runs yt-dlp with the flags ported from the Museeks
// implementation: MP3 at 192K with embedded ID3 tags and cover art, and a
// --print template that reports the final path and metadata on stdout.
func (i *importer) downloadAudio(ctx context.Context, rawURL, destination string) (*Result, error) {
	printTemplate := "after_move:%(filepath)s" + separator + "%(title)s" + separator +
		"%(artist,uploader)s" + separator + "%(duration)s"

	stdout, stderr, err := i.run(ctx,
		// YouTube extraction needs a JavaScript runtime; allow whichever of
		// deno (yt-dlp's default) or node is installed
		"--js-runtimes", "deno",
		"--js-runtimes", "node",
		"--no-playlist",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "192K",
		"--embed-metadata",
		"--embed-thumbnail",
		"--output", filepath.Join(destination, "%(title)s.%(ext)s"),
		"--no-simulate",
		"--quiet",
		"--print", printTemplate,
		"--", rawURL,
	)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return nil, ErrYtdlpNotFound
		}
		detail := stderr
		for _, line := range strings.Split(stderr, "\n") {
			if strings.HasPrefix(line, "ERROR") {
				detail = line
				break
			}
		}
		return nil, &DownloadFailedError{Detail: strings.TrimSpace(detail)}
	}

	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, separator) {
			continue
		}
		fields := strings.Split(line, separator)
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected yt-dlp output: %q", line)
		}
		duration, _ := strconv.ParseFloat(fields[3], 64)
		return &Result{
			Path:     fields[0],
			Title:    fields[1],
			Artist:   fields[2],
			Duration: int(duration + 0.5),
		}, nil
	}
	return nil, fmt.Errorf("unexpected yt-dlp output: %q", stdout)
}

// fetchLyrics queries LRCLIB for the downloaded track and writes a .lrc
// sidecar next to the MP3 when lyrics are found. Failures are logged and
// swallowed: the import already succeeded.
func (i *importer) fetchLyrics(ctx context.Context, result *Result) {
	q := url.Values{}
	q.Set("track_name", result.Title)
	q.Set("artist_name", result.Artist)
	q.Set("duration", strconv.Itoa(result.Duration))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.lrclibBaseURL+"/api/get?"+q.Encode(), nil)
	if err != nil {
		log.Warn(ctx, "ytimport: building LRCLIB request failed", err)
		return
	}
	req.Header.Set("User-Agent", lrclibUserAgent)

	resp, err := i.httpClient.Do(req)
	if err != nil {
		log.Warn(ctx, "ytimport: LRCLIB request failed", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Warn(ctx, "ytimport: LRCLIB returned an error", "status", resp.StatusCode)
		return
	}

	var payload struct {
		SyncedLyrics string `json:"syncedLyrics"`
		PlainLyrics  string `json:"plainLyrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Warn(ctx, "ytimport: decoding LRCLIB response failed", err)
		return
	}

	lyrics := payload.SyncedLyrics
	synced := true
	if lyrics == "" {
		lyrics = payload.PlainLyrics
		synced = false
	}
	if lyrics == "" {
		return
	}

	ext := filepath.Ext(result.Path)
	sidecar := strings.TrimSuffix(result.Path, ext) + ".lrc"
	if err := os.WriteFile(sidecar, []byte(lyrics), 0o644); err != nil {
		log.Warn(ctx, "ytimport: writing .lrc sidecar failed", "path", sidecar, err)
		return
	}
	result.LyricsFound = true
	result.LyricsSynced = synced
}

func runYtdlp(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "yt-dlp", args...) // #nosec
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
