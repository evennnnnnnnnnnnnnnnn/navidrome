package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Deletion", func() {
	var (
		ds      *tests.MockDataStore
		repo    *tests.MockMediaFileRepo
		service Maintenance
		ctx     context.Context
		libRoot string
		trash   string
	)

	// writeTrack creates a real file under the library and returns the record pointing at it.
	writeTrack := func(id, relPath string) model.MediaFile {
		full := filepath.Join(libRoot, relPath)
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, []byte("audio-"+id), 0o644)).To(Succeed())
		return model.MediaFile{ID: id, LibraryID: 1, LibraryPath: libRoot, Path: relPath, AlbumID: "album1"}
	}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		libRoot = GinkgoT().TempDir()
		trash = filepath.Join(GinkgoT().TempDir(), "trash")
		conf.Server.Deletion.Enabled = true
		conf.Server.Deletion.TrashFolder = trash

		ctx = request.WithUser(context.Background(), model.User{ID: "u1", IsAdmin: true})
		repo = tests.CreateMockMediaFileRepo()
		ds = &tests.MockDataStore{MockedMediaFile: repo}
		service = NewMaintenance(ds)
	})

	Describe("gating", func() {
		It("refuses when the feature is disabled", func() {
			conf.Server.Deletion.Enabled = false
			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).To(MatchError(ErrDeletionDisabled))
		})

		It("refuses a non-admin user", func() {
			userCtx := request.WithUser(context.Background(), model.User{ID: "u2", IsAdmin: false})
			_, err := service.DeleteMediaFiles(userCtx, []string{"mf1"})
			Expect(err).To(MatchError(ErrDeletionNotAdmin))
		})

		It("refuses a request with no user at all", func() {
			_, err := service.DeleteMediaFiles(context.Background(), []string{"mf1"})
			Expect(err).To(MatchError(ErrDeletionNotAdmin))
		})

		It("refuses an empty id list instead of deleting everything", func() {
			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})
			_, err := service.DeleteMediaFiles(ctx, nil)
			Expect(err).To(MatchError(ErrNoIDs))
			Expect(repo.Data).To(HaveKey("mf1"))
			Expect(filepath.Join(libRoot, "a.mp3")).To(BeAnExistingFile())
		})

		It("refuses an empty album id list", func() {
			_, err := service.DeleteAlbums(ctx, nil)
			Expect(err).To(MatchError(ErrNoIDs))
		})

		It("returns not found when no rows match", func() {
			repo.SetData(model.MediaFiles{})
			_, err := service.DeleteMediaFiles(ctx, []string{"nope"})
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	Describe("deleting songs", func() {
		It("moves the file to trash and removes the row", func() {
			mf := writeTrack("mf1", filepath.Join("Artist", "Album", "track.mp3"))
			repo.SetData(model.MediaFiles{mf})

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())

			Expect(result.DeletedIDs).To(ConsistOf("mf1"))
			Expect(result.Count).To(Equal(1))
			Expect(filepath.Join(libRoot, "Artist", "Album", "track.mp3")).ToNot(BeAnExistingFile())

			moved := filepath.Join(result.TrashFolder, "library-1", "Artist", "Album", "track.mp3")
			Expect(moved).To(BeAnExistingFile())
			Expect(os.ReadFile(moved)).To(Equal([]byte("audio-mf1")))

			Expect(repo.Data).ToNot(HaveKey("mf1"))
			Expect(ds.GCCalled).To(BeTrue())
		})

		It("writes a manifest describing where each file came from", func() {
			mf := writeTrack("mf1", filepath.Join("Artist", "track.mp3"))
			repo.SetData(model.MediaFiles{mf})

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())

			body, err := os.ReadFile(filepath.Join(result.TrashFolder, "manifest.json"))
			Expect(err).ToNot(HaveOccurred())

			var manifest struct {
				Files []trashEntry `json:"files"`
			}
			Expect(json.Unmarshal(body, &manifest)).To(Succeed())
			Expect(manifest.Files).To(HaveLen(1))
			Expect(manifest.Files[0].ID).To(Equal("mf1"))
			Expect(manifest.Files[0].LibraryID).To(Equal(1))
			Expect(manifest.Files[0].OriginalPath).To(Equal(filepath.Join(libRoot, "Artist", "track.mp3")))
		})

		It("filters the query by the requested ids", func() {
			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})
			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(repo.Options.Filters).ToNot(BeNil())
			sql, args, err := repo.Options.Filters.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(ContainSubstring("media_file.id"))
			Expect(args).To(ConsistOf("mf1"))
		})

		It("deletes several files in one batch", func() {
			repo.SetData(model.MediaFiles{
				writeTrack("mf1", filepath.Join("A", "1.mp3")),
				writeTrack("mf2", filepath.Join("A", "2.mp3")),
			})

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1", "mf2"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("mf1", "mf2"))
			Expect(filepath.Join(result.TrashFolder, "library-1", "A", "1.mp3")).To(BeAnExistingFile())
			Expect(filepath.Join(result.TrashFolder, "library-1", "A", "2.mp3")).To(BeAnExistingFile())
			Expect(repo.Data).To(BeEmpty())
		})

		It("removes the row for a file that is already gone from disk", func() {
			repo.SetData(model.MediaFiles{
				{ID: "gone", LibraryID: 1, LibraryPath: libRoot, Path: "vanished.mp3", AlbumID: "album1"},
			})

			result, err := service.DeleteMediaFiles(ctx, []string{"gone"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("gone"))
			Expect(repo.Data).ToNot(HaveKey("gone"))
		})

		It("does not create a trash folder when there is nothing to move", func() {
			repo.SetData(model.MediaFiles{
				{ID: "gone", LibraryID: 1, LibraryPath: libRoot, Path: "vanished.mp3", AlbumID: "album1"},
			})

			result, err := service.DeleteMediaFiles(ctx, []string{"gone"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.TrashFolder).To(BeEmpty())
			Expect(trash).ToNot(BeADirectory(), "a rows-only cleanup must not litter the trash")
		})

		It("keeps each delete request in its own trash folder", func() {
			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})
			first, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())

			repo.SetData(model.MediaFiles{writeTrack("mf2", "a.mp3")})
			second, err := service.DeleteMediaFiles(ctx, []string{"mf2"})
			Expect(err).ToNot(HaveOccurred())

			Expect(second.TrashFolder).ToNot(Equal(first.TrashFolder))
			Expect(filepath.Join(first.TrashFolder, "library-1", "a.mp3")).To(BeAnExistingFile())
			Expect(filepath.Join(second.TrashFolder, "library-1", "a.mp3")).To(BeAnExistingFile())
		})
	})

	Describe("deleting albums", func() {
		It("deletes every track of the album", func() {
			repo.SetData(model.MediaFiles{
				writeTrack("mf1", filepath.Join("Album", "1.mp3")),
				writeTrack("mf2", filepath.Join("Album", "2.mp3")),
			})

			result, err := service.DeleteAlbums(ctx, []string{"album1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("mf1", "mf2"))
			Expect(repo.Data).To(BeEmpty())
			Expect(ds.GCCalled).To(BeTrue(), "GC reaps the now-empty album row")
		})

		It("filters the query by album id", func() {
			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})
			_, err := service.DeleteAlbums(ctx, []string{"album1"})
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := repo.Options.Filters.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(ContainSubstring("album_id"))
			Expect(args).To(ConsistOf("album1"))
		})
	})

	Describe("path safety", func() {
		var outsider string

		BeforeEach(func() {
			outsider = filepath.Join(GinkgoT().TempDir(), "precious.mp3")
			Expect(os.WriteFile(outsider, []byte("do not touch"), 0o644)).To(Succeed())
		})

		It("refuses a path that climbs out of the library", func() {
			rel, err := filepath.Rel(libRoot, outsider)
			Expect(err).ToNot(HaveOccurred())
			repo.SetData(model.MediaFiles{
				{ID: "escape", LibraryID: 1, LibraryPath: libRoot, Path: rel},
			})

			_, err = service.DeleteMediaFiles(ctx, []string{"escape"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(outsider).To(BeAnExistingFile())
			Expect(repo.Data).To(HaveKey("escape"), "the row survives a refused delete")
		})

		It("refuses a symlink pointing outside the library", func() {
			link := filepath.Join(libRoot, "link.mp3")
			Expect(os.Symlink(outsider, link)).To(Succeed())
			repo.SetData(model.MediaFiles{
				{ID: "link", LibraryID: 1, LibraryPath: libRoot, Path: "link.mp3"},
			})

			_, err := service.DeleteMediaFiles(ctx, []string{"link"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(outsider).To(BeAnExistingFile())
			Expect(link).To(BeAnExistingFile())
		})

		It("refuses a record with no library path", func() {
			repo.SetData(model.MediaFiles{{ID: "nolib", Path: "a.mp3"}})
			_, err := service.DeleteMediaFiles(ctx, []string{"nolib"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
		})

		It("refuses a directory", func() {
			Expect(os.MkdirAll(filepath.Join(libRoot, "adir"), 0o755)).To(Succeed())
			repo.SetData(model.MediaFiles{
				{ID: "dir", LibraryID: 1, LibraryPath: libRoot, Path: "adir"},
			})
			_, err := service.DeleteMediaFiles(ctx, []string{"dir"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(filepath.Join(libRoot, "adir")).To(BeADirectory())
		})

		It("refuses when the trash folder sits inside the music folder", func() {
			conf.Server.Deletion.TrashFolder = filepath.Join(libRoot, ".trash")
			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})

			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(filepath.Join(libRoot, "a.mp3")).To(BeAnExistingFile())
		})

		It("aborts the whole batch when one file is unsafe", func() {
			good := writeTrack("good", "good.mp3")
			rel, err := filepath.Rel(libRoot, outsider)
			Expect(err).ToNot(HaveOccurred())
			repo.SetData(model.MediaFiles{
				good,
				{ID: "bad", LibraryID: 1, LibraryPath: libRoot, Path: rel},
			})

			_, err = service.DeleteMediaFiles(ctx, []string{"good", "bad"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(filepath.Join(libRoot, "good.mp3")).To(BeAnExistingFile(),
				"a bad entry must not take the good ones with it")
			Expect(repo.Data).To(HaveKey("good"))
		})
	})

	Describe("partial failures", func() {
		// A batch that stops halfway must never be reported as a completed one, or the UI
		// tells the admin "deleted 1" when they asked for 3 and two are still there.
		It("reports an error when a move fails partway through", func() {
			locked := filepath.Join(libRoot, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			victim := writeTrack("stuck", filepath.Join("locked", "stuck.mp3"))
			ok := writeTrack("ok", "ok.mp3")
			// A read-only parent blocks the unlink, so the move cannot complete.
			Expect(os.Chmod(locked, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

			repo.SetData(model.MediaFiles{ok, victim})

			_, err := service.DeleteMediaFiles(ctx, []string{"ok", "stuck"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("stopped"))
		})

		It("still reports an error when only already-missing rows succeeded", func() {
			locked := filepath.Join(libRoot, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			victim := writeTrack("stuck", filepath.Join("locked", "stuck.mp3"))
			Expect(os.Chmod(locked, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

			repo.SetData(model.MediaFiles{
				{ID: "gone", LibraryID: 1, LibraryPath: libRoot, Path: "vanished.mp3", AlbumID: "album1"},
				victim,
			})

			_, err := service.DeleteMediaFiles(ctx, []string{"gone", "stuck"})
			Expect(err).To(HaveOccurred(), "a rows-only success must not mask a failed move")
			Expect(filepath.Join(libRoot, "locked", "stuck.mp3")).To(BeAnExistingFile())
		})
	})

	Describe("multi-library trash safety", func() {
		// The trash is global config, so it has to be checked against every music folder -
		// not only the ones the current request happens to touch.
		It("refuses when the trash sits inside a library the request does not touch", func() {
			otherLib := GinkgoT().TempDir()
			conf.Server.Deletion.TrashFolder = filepath.Join(otherLib, "trash")

			libRepo := &tests.MockLibraryRepo{}
			libRepo.SetData(model.Libraries{
				{ID: 1, Name: "One", Path: libRoot},
				{ID: 2, Name: "Two", Path: otherLib},
			})
			ds.MockedLibrary = libRepo

			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})

			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(filepath.Join(libRoot, "a.mp3")).To(BeAnExistingFile())
		})

		It("refuses when a symlinked library actually contains the trash", func() {
			realLib := GinkgoT().TempDir()
			link := filepath.Join(GinkgoT().TempDir(), "libLink")
			Expect(os.Symlink(realLib, link)).To(Succeed())
			conf.Server.Deletion.TrashFolder = filepath.Join(realLib, "trash")
			Expect(os.MkdirAll(conf.Server.Deletion.TrashFolder, 0o755)).To(Succeed())

			libRepo := &tests.MockLibraryRepo{}
			libRepo.SetData(model.Libraries{{ID: 1, Name: "Linked", Path: link}})
			ds.MockedLibrary = libRepo

			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.mp3")})

			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
		})
	})

	Describe("moveFile", func() {
		It("leaves no copy behind when the original cannot be unlinked", func() {
			locked := filepath.Join(libRoot, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			src := filepath.Join(locked, "stuck.mp3")
			Expect(os.WriteFile(src, []byte("payload"), 0o644)).To(Succeed())
			Expect(os.Chmod(locked, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

			dest := filepath.Join(trash, "stuck.mp3")
			Expect(moveFile(src, dest)).ToNot(Succeed())
			Expect(src).To(BeAnExistingFile(), "the original stays put")
			Expect(dest).ToNot(BeAnExistingFile(), "and no phantom copy is left in the trash")
		})

		It("refuses to overwrite a file already in the trash", func() {
			src := filepath.Join(libRoot, "src.mp3")
			Expect(os.WriteFile(src, []byte("new"), 0o644)).To(Succeed())
			dest := filepath.Join(trash, "dest.mp3")
			Expect(os.MkdirAll(trash, 0o755)).To(Succeed())
			Expect(os.WriteFile(dest, []byte("PRECIOUS"), 0o644)).To(Succeed())

			Expect(moveFile(src, dest)).ToNot(Succeed())
			Expect(os.ReadFile(dest)).To(Equal([]byte("PRECIOUS")))
			Expect(src).To(BeAnExistingFile())
		})

		It("falls back to copy when rename is not possible", func() {
			src := filepath.Join(libRoot, "src.mp3")
			Expect(os.WriteFile(src, []byte("payload"), 0o644)).To(Succeed())
			dest := filepath.Join(GinkgoT().TempDir(), "nested", "dest.mp3")

			// copyFile is the fallback half of moveFile; exercise it directly since a real
			// cross-device move cannot be forced in a unit test.
			Expect(os.MkdirAll(filepath.Dir(dest), 0o755)).To(Succeed())
			Expect(copyFile(src, dest)).To(Succeed())
			Expect(os.ReadFile(dest)).To(Equal([]byte("payload")))
			Expect(src).To(BeAnExistingFile(), "copyFile leaves the source alone")
		})

		It("creates missing parent folders", func() {
			src := filepath.Join(libRoot, "src.mp3")
			Expect(os.WriteFile(src, []byte("payload"), 0o644)).To(Succeed())
			dest := filepath.Join(trash, "a", "b", "c", "dest.mp3")

			Expect(moveFile(src, dest)).To(Succeed())
			Expect(dest).To(BeAnExistingFile())
			Expect(src).ToNot(BeAnExistingFile())
		})

		// Only EXDEV earns the copy fallback. Copying after a permission error, a busy
		// file or a source that vanished is a second and weaker attempt at the same move -
		// and the copy path is the one that can be redirected at a symlink.
		It("reports the rename error instead of copying when the failure is not cross-device", func() {
			locked := filepath.Join(libRoot, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			src := filepath.Join(locked, "stuck.mp3")
			Expect(os.WriteFile(src, []byte("payload"), 0o644)).To(Succeed())
			Expect(os.Chmod(locked, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

			dest := filepath.Join(trash, "stuck.mp3")
			err := moveFile(src, dest)
			Expect(err).To(HaveOccurred())

			var linkErr *os.LinkError
			Expect(errors.As(err, &linkErr)).To(BeTrue(), "the rename error must be surfaced, not a later copy error")
			Expect(linkErr.Op).To(Equal("rename"))
			Expect(dest).ToNot(BeAnExistingFile(), "no copy is attempted at all")
		})

		It("reports the rename error for a source that is not there", func() {
			err := moveFile(filepath.Join(libRoot, "ghost.mp3"), filepath.Join(trash, "ghost.mp3"))
			Expect(err).To(MatchError(os.ErrNotExist))
			var linkErr *os.LinkError
			Expect(errors.As(err, &linkErr)).To(BeTrue())
		})
	})

	Describe("copyFile", func() {
		// resolveLibraryFile rejects symlinks, but that check and the copy are separated by
		// the rest of the planning loop. Anything with write access to the music folder can
		// use that window to swap a track for a link to a file the server can read - and
		// since the trash is normally on another device, the copy path is the one that runs.
		It("refuses to copy through a symlink", func() {
			secret := filepath.Join(GinkgoT().TempDir(), "navidrome.db")
			Expect(os.WriteFile(secret, []byte("SECRET"), 0o644)).To(Succeed())
			link := filepath.Join(libRoot, "swapped.mp3")
			Expect(os.Symlink(secret, link)).To(Succeed())

			dest := filepath.Join(trash, "swapped.mp3")
			Expect(os.MkdirAll(trash, 0o755)).To(Succeed())

			Expect(copyFile(link, dest)).ToNot(Succeed())
			Expect(dest).ToNot(BeAnExistingFile(), "the symlink target must never reach the trash")
			Expect(os.ReadFile(secret)).To(Equal([]byte("SECRET")))
		})

		It("refuses to copy anything that is not a regular file", func() {
			dir := filepath.Join(libRoot, "adir")
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			Expect(os.MkdirAll(trash, 0o755)).To(Succeed())

			err := copyFile(dir, filepath.Join(trash, "adir"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a regular file"))
		})
	})

	Describe("empty trash batches", func() {
		// A batch folder whose first move failed holds nothing and has no manifest, but is
		// indistinguishable from a real deletion to anyone reading the trash root later.
		It("removes the batch folder when nothing could be moved", func() {
			locked := filepath.Join(libRoot, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			victim := writeTrack("stuck", filepath.Join("locked", "stuck.mp3"))
			Expect(os.Chmod(locked, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

			repo.SetData(model.MediaFiles{victim})

			_, err := service.DeleteMediaFiles(ctx, []string{"stuck"})
			Expect(err).To(HaveOccurred())

			entries, err := os.ReadDir(trash)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(BeEmpty(), "a failed batch must not leave a folder in the trash")
		})

		It("keeps the batch folder when at least one file made it", func() {
			locked := filepath.Join(libRoot, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			victim := writeTrack("stuck", filepath.Join("locked", "stuck.mp3"))
			good := writeTrack("good", "good.mp3")
			Expect(os.Chmod(locked, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

			// "good" sorts first, so it is moved before the failure aborts the batch.
			repo.SetData(model.MediaFiles{good, victim})

			_, err := service.DeleteMediaFiles(ctx, []string{"good", "stuck"})
			Expect(err).To(HaveOccurred())

			entries, err := os.ReadDir(trash)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(HaveLen(1), "the batch holding the moved file must survive")
		})

		Describe("removeEmptyTree", func() {
			It("removes a nest of empty folders", func() {
				// This is the real shape of a failed batch: moveFile creates the
				// destination's parents before it tries the move.
				root := filepath.Join(GinkgoT().TempDir(), "batch")
				Expect(os.MkdirAll(filepath.Join(root, "library-1", "Artist", "Album"), 0o755)).To(Succeed())

				Expect(removeEmptyTree(root)).To(Succeed())
				Expect(root).ToNot(BeADirectory())
			})

			It("refuses to touch a tree containing a file", func() {
				root := filepath.Join(GinkgoT().TempDir(), "batch")
				nested := filepath.Join(root, "library-1", "Artist")
				Expect(os.MkdirAll(nested, 0o755)).To(Succeed())
				keep := filepath.Join(nested, "track.mp3")
				Expect(os.WriteFile(keep, []byte("PRECIOUS"), 0o644)).To(Succeed())

				Expect(removeEmptyTree(root)).ToNot(Succeed())
				Expect(os.ReadFile(keep)).To(Equal([]byte("PRECIOUS")))
				Expect(root).To(BeADirectory())
			})

			It("refuses to descend through a symlink", func() {
				outside := GinkgoT().TempDir()
				Expect(os.WriteFile(filepath.Join(outside, "precious.mp3"), []byte("x"), 0o644)).To(Succeed())
				root := filepath.Join(GinkgoT().TempDir(), "batch")
				Expect(os.MkdirAll(root, 0o755)).To(Succeed())
				Expect(os.Symlink(outside, filepath.Join(root, "link"))).To(Succeed())

				Expect(removeEmptyTree(root)).ToNot(Succeed())
				Expect(filepath.Join(outside, "precious.mp3")).To(BeAnExistingFile())
			})
		})
	})

	Describe("trash capacity", func() {
		// The trash defaults to <DataFolder>/trash, which in the standard Docker layout is
		// a different and much smaller volume than the music folder. Neither a second
		// device nor a nearly-full one can be staged in a unit test, so the two syscalls
		// are stubbed and everything above them is the real code.
		const (
			musicDev = uint64(1)
			trashDev = uint64(2)
		)

		stubVolumes := func(free uint64, libDev uint64) {
			origFree, origDev := fsFreeBytes, fsDeviceID
			DeferCleanup(func() { fsFreeBytes, fsDeviceID = origFree, origDev })
			fsFreeBytes = func(string) (uint64, error) { return free, nil }
			fsDeviceID = func(path string) (uint64, error) {
				if strings.HasPrefix(path, libRoot) {
					return libDev, nil
				}
				return trashDev, nil
			}
		}

		sized := func(id, relPath string, size int64) model.MediaFile {
			mf := writeTrack(id, relPath)
			mf.Size = size
			return mf
		}

		It("refuses the whole request when the copy would not fit", func() {
			stubVolumes(1<<20, musicDev) // 1 MiB free, 512 MiB to copy
			repo.SetData(model.MediaFiles{sized("mf1", "big.flac", 512<<20)})

			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(err.Error()).To(ContainSubstring("free"))

			Expect(filepath.Join(libRoot, "big.flac")).To(BeAnExistingFile(), "nothing is moved when the request is refused")
			Expect(repo.Data).To(HaveKey("mf1"))
			Expect(trash).ToNot(BeADirectory(), "a refused request must not claim a batch folder")
		})

		It("sums the whole batch, not just the largest file", func() {
			stubVolumes(150<<20, musicDev) // each fits on its own, together they do not
			repo.SetData(model.MediaFiles{
				sized("mf1", "a.flac", 60<<20),
				sized("mf2", "b.flac", 60<<20),
			})

			_, err := service.DeleteMediaFiles(ctx, []string{"mf1", "mf2"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
			Expect(repo.Data).To(And(HaveKey("mf1"), HaveKey("mf2")))
		})

		It("refuses a request that would fit exactly, leaving no headroom", func() {
			// The volume holding the trash also holds navidrome.db and its WAL, so filling
			// it to the last byte breaks every write that follows a successful copy.
			stubVolumes(10<<20, musicDev)
			repo.SetData(model.MediaFiles{sized("mf1", "a.flac", 10<<20)})

			_, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).To(MatchError(ErrUnsafeDeletion))
		})

		It("allows a request with room to spare", func() {
			stubVolumes(4<<30, musicDev) // 4 GiB free
			repo.SetData(model.MediaFiles{sized("mf1", "a.flac", 10<<20)})

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("mf1"))
		})

		It("does not count files that are only being renamed", func() {
			// Same device: the move is a rename and costs nothing, so a single-volume
			// install must still be able to delete an album bigger than its free space -
			// which is usually the whole point of deleting it.
			stubVolumes(1, trashDev)
			repo.SetData(model.MediaFiles{sized("mf1", "huge.flac", 1<<40)})

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("mf1"))
		})

		It("proceeds when the filesystem cannot be measured", func() {
			// No statfs on this platform, or the folder moved under us. The check is a
			// safety net, not a gate.
			origFree, origDev := fsFreeBytes, fsDeviceID
			DeferCleanup(func() { fsFreeBytes, fsDeviceID = origFree, origDev })
			fsFreeBytes = func(string) (uint64, error) { return 0, errors.New("not supported") }
			fsDeviceID = func(string) (uint64, error) { return 0, errors.New("not supported") }

			repo.SetData(model.MediaFiles{sized("mf1", "a.flac", 1<<40)})

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("mf1"))
		})

		It("does not refuse a batch whose sizes were never recorded", func() {
			stubVolumes(1<<20, musicDev)
			repo.SetData(model.MediaFiles{writeTrack("mf1", "a.flac")}) // Size stays 0

			result, err := service.DeleteMediaFiles(ctx, []string{"mf1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.DeletedIDs).To(ConsistOf("mf1"))
		})
	})

	Describe("nearestExistingDir", func() {
		It("returns the folder itself when it exists", func() {
			Expect(nearestExistingDir(libRoot)).To(Equal(libRoot))
		})

		It("walks up to the first folder that exists", func() {
			// The trash folder is only created on the first delete, so the check has to
			// measure a parent instead.
			Expect(nearestExistingDir(filepath.Join(libRoot, "a", "b", "c"))).To(Equal(libRoot))
		})

		It("does not mistake a file for a folder", func() {
			file := filepath.Join(libRoot, "a.mp3")
			Expect(os.WriteFile(file, []byte("x"), 0o644)).To(Succeed())
			Expect(nearestExistingDir(file)).To(Equal(libRoot))
		})
	})
})
