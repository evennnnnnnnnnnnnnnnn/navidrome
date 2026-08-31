package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/dustin/go-humanize"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/utils"
)

var (
	// ErrDeletionDisabled is returned when Deletion.Enabled is false. Deleting media is
	// opt-in per server, so this is the normal answer on a default install.
	ErrDeletionDisabled = errors.New("deleting media files is disabled on this server")
	// ErrDeletionNotAdmin is returned when a non-admin reaches the service. The HTTP layer
	// already gates on admin; this is the second lock on the same door.
	ErrDeletionNotAdmin = errors.New("only admins can delete media files")
	// ErrNoIDs is returned for an empty id list. Unlike the missing-files endpoint, "no
	// ids" must never be read as "everything".
	ErrNoIDs = errors.New("no ids given")
	// ErrUnsafeDeletion wraps every refusal to touch a path: outside its library, not a
	// regular file, or a trash folder that would be rescanned. Nothing has been moved when
	// this is returned.
	ErrUnsafeDeletion = errors.New("unsafe deletion")
)

// DeletionResult reports what a delete request actually did.
type DeletionResult struct {
	// DeletedIDs are the media file ids whose row was removed from the DB.
	DeletedIDs []string `json:"ids"`
	// TrashFolder is the batch folder the files were moved into.
	TrashFolder string `json:"trashFolder"`
	// Count is len(DeletedIDs), for convenience in the UI.
	Count int `json:"count"`
}

// DeleteMediaFiles moves the given media files to the trash folder and removes their rows.
//
// Validation happens for every file before anything is moved, so a request naming one bad
// path leaves the whole library untouched rather than half-deleting it.
func (s *maintenanceService) DeleteMediaFiles(ctx context.Context, ids []string) (*DeletionResult, error) {
	if err := checkDeletionAllowed(ctx); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrNoIDs
	}

	mfs, err := s.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Eq{"media_file.id": ids},
	})
	if err != nil {
		return nil, fmt.Errorf("loading media files: %w", err)
	}
	if len(mfs) == 0 {
		return nil, model.ErrNotFound
	}

	return s.deleteMediaFiles(ctx, mfs)
}

// DeleteAlbums deletes every track of the given albums. The album rows themselves are
// reaped by GC once they have no tracks left, the same way the scanner does it.
func (s *maintenanceService) DeleteAlbums(ctx context.Context, albumIDs []string) (*DeletionResult, error) {
	if err := checkDeletionAllowed(ctx); err != nil {
		return nil, err
	}
	if len(albumIDs) == 0 {
		return nil, ErrNoIDs
	}

	mfs, err := s.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Eq{"album_id": albumIDs},
	})
	if err != nil {
		return nil, fmt.Errorf("loading album tracks: %w", err)
	}
	if len(mfs) == 0 {
		return nil, model.ErrNotFound
	}

	return s.deleteMediaFiles(ctx, mfs)
}

func (s *maintenanceService) deleteMediaFiles(ctx context.Context, mfs model.MediaFiles) (*DeletionResult, error) {
	trashRoot, err := trashRoot()
	if err != nil {
		return nil, err
	}

	// The trash must sit outside *every* music folder, not just the ones this request
	// happens to touch: dropping files into another library's folder would have the next
	// scan import them straight back.
	if err := s.checkTrashOutsideEveryLibrary(ctx, trashRoot); err != nil {
		return nil, err
	}

	// Resolve and validate every path up front. Anything suspicious aborts the request
	// before a single file has moved.
	planned := make([]plannedMove, 0, len(mfs))
	// Files already gone from disk still get their row removed - that is the missing-file
	// cleanup case, and there is nothing to move.
	rowsOnly := make([]string, 0)

	for _, mf := range mfs {
		src, err := resolveLibraryFile(mf)
		if errors.Is(err, os.ErrNotExist) {
			log.Debug(ctx, "Media file already gone from disk, removing its row only", "id", mf.ID, "path", mf.Path)
			rowsOnly = append(rowsOnly, mf.ID)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: refusing to delete %q: %s", ErrUnsafeDeletion, mf.Path, err)
		}
		// Re-check against this file's own library path as well. The sweep above covers the
		// library table; this covers the path actually about to be moved out of, so the
		// guard still holds if the two ever disagree.
		if err := checkTrashOutsideLibrary(trashRoot, mf.LibraryPath); err != nil {
			return nil, err
		}
		planned = append(planned, plannedMove{
			mf:  mf,
			src: src,
			rel: filepath.Join(fmt.Sprintf("library-%d", mf.LibraryID), filepath.FromSlash(mf.Path)),
		})
	}

	// The last check that can still refuse the whole request. Everything below this line
	// starts touching files.
	if err := checkTrashCapacity(ctx, trashRoot, planned); err != nil {
		return nil, err
	}

	// Only claim a trash folder once there is something to put in it, so a rows-only
	// cleanup does not litter the trash with empty batches.
	var batch string
	if len(planned) > 0 {
		batch, err = newTrashBatch(trashRoot)
		if err != nil {
			return nil, err
		}
	}

	moved := make([]string, 0, len(planned))
	entries := make([]trashEntry, 0, len(planned))
	var moveErr error
	for _, p := range planned {
		dest := filepath.Join(batch, p.rel)
		// Belt and braces: the source check already rules this out, but a destination that
		// escapes the batch folder must never be written to.
		if err := utils.CheckPathContained(batch, dest); err != nil {
			moveErr = fmt.Errorf("%w: %s", ErrUnsafeDeletion, err)
			break
		}
		if err := moveFile(p.src, dest); err != nil {
			// Stop rather than pressing on. The rows for everything already moved still get
			// removed below, so disk and DB stay consistent, and the error is reported so a
			// partial batch is never mistaken for a complete one.
			log.Error(ctx, "Error moving media file to trash, aborting the rest of the batch",
				"id", p.mf.ID, "src", p.src, "dest", dest, err)
			moveErr = fmt.Errorf("moving %q to trash: %w", p.mf.Path, err)
			break
		}
		moved = append(moved, p.mf.ID)
		entries = append(entries, trashEntry{ID: p.mf.ID, LibraryID: p.mf.LibraryID, OriginalPath: p.src, TrashPath: dest})
	}

	deleted := append(moved, rowsOnly...) //nolint:gocritic // a fresh slice is intended here

	if len(entries) > 0 {
		if err := writeTrashManifest(batch, entries); err != nil {
			// A missing manifest makes recovery harder but does not make the delete wrong.
			log.Warn(ctx, "Error writing trash manifest", "batch", batch, err)
		}
	} else if batch != "" {
		// The very first move failed, so this batch holds no files and has no manifest.
		// Left behind it accumulates in the trash root as a timestamped folder
		// indistinguishable from a real deletion, which is exactly the wrong thing to hand
		// someone trying to restore.
		if err := removeEmptyTree(batch); err != nil {
			log.Warn(ctx, "Error removing empty trash batch folder", "batch", batch, err)
		}
		batch = ""
	}

	affectedAlbumIDs := albumIDsOf(mfs, deleted)

	// Detach from the request context. The files are already in the trash; if the client
	// disconnects now, cancelling the row delete would leave the library pointing at music
	// that is no longer there.
	bgCtx := request.AddValues(context.Background(), ctx)

	if len(deleted) > 0 {
		err = s.ds.WithTx(func(tx model.DataStore) error {
			for _, id := range deleted {
				if err := tx.MediaFile(bgCtx).Delete(id); err != nil && !errors.Is(err, model.ErrNotFound) {
					return err
				}
			}
			return nil
		})
		if err != nil {
			log.Error(ctx, "Error deleting media files from DB after moving them to trash",
				"ids", deleted, "batch", batch, err)
			if batch == "" {
				// Nothing was moved - these were rows for files already gone from disk.
				return nil, fmt.Errorf("the media files could not be removed from the library: %w", err)
			}
			return nil, fmt.Errorf("the files were moved to %s but their entries could not be removed: %w", batch, err)
		}

		// The rows are gone, which is the part that matters. A failed GC only leaves
		// orphaned album/artist rows behind, and the next scan cleans those up.
		if err := s.ds.GC(bgCtx); err != nil {
			log.Error(ctx, "Error running GC after deleting media files", err)
		}

		s.refreshStatsAsync(ctx, affectedAlbumIDs)
	}

	if moveErr != nil {
		return nil, fmt.Errorf("deleted %d of %d, then stopped: %w", len(deleted), len(mfs), moveErr)
	}

	log.Info(ctx, "Deleted media files", "count", len(deleted), "trash", batch)
	return &DeletionResult{DeletedIDs: deleted, TrashFolder: batch, Count: len(deleted)}, nil
}

// checkTrashOutsideEveryLibrary refuses to run when the trash folder sits inside any music
// folder, which would make the next scan re-import everything just deleted.
func (s *maintenanceService) checkTrashOutsideEveryLibrary(ctx context.Context, trashRoot string) error {
	libs, err := s.ds.Library(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("loading libraries: %w", err)
	}
	for _, lib := range libs {
		if err := checkTrashOutsideLibrary(trashRoot, lib.Path); err != nil {
			return err
		}
	}
	return nil
}

func checkDeletionAllowed(ctx context.Context) error {
	if !conf.Server.Deletion.Enabled {
		return ErrDeletionDisabled
	}
	user, ok := request.UserFrom(ctx)
	if !ok || !user.IsAdmin {
		return ErrDeletionNotAdmin
	}
	return nil
}

func albumIDsOf(mfs model.MediaFiles, deletedIDs []string) []string {
	wanted := make(map[string]struct{}, len(deletedIDs))
	for _, id := range deletedIDs {
		wanted[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(mfs))
	ids := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		if _, ok := wanted[mf.ID]; !ok {
			continue
		}
		if mf.AlbumID == "" {
			continue
		}
		if _, dup := seen[mf.AlbumID]; dup {
			continue
		}
		seen[mf.AlbumID] = struct{}{}
		ids = append(ids, mf.AlbumID)
	}
	return ids
}

// resolveLibraryFile turns a media file record into an absolute path that is provably
// inside its library, or an error explaining why it is not safe to touch.
//
// The path comes entirely from the stored record, never from client input, but a corrupt
// Path column or a symlinked folder could still point outside the library - so both the
// lexical and the symlink-resolved forms are checked. Returns os.ErrNotExist when the file
// is simply gone, which callers treat as "remove the row only".
func resolveLibraryFile(mf model.MediaFile) (string, error) {
	if mf.LibraryPath == "" {
		return "", errors.New("media file has no library path")
	}
	if mf.Path == "" {
		return "", errors.New("media file has no path")
	}

	root, err := filepath.Abs(mf.LibraryPath)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(mf.AbsolutePath())
	if err != nil {
		return "", err
	}
	if err := utils.CheckPathContained(root, target); err != nil {
		return "", err
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", os.ErrNotExist
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%q is a symlink", mf.Path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", mf.Path)
	}

	// Re-check containment after resolving symlinks, so a symlinked parent folder cannot
	// be used to reach a file outside the library. Both paths exist (Lstat just succeeded),
	// so an error here is unexpected and refuses the delete rather than skipping the check.
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolving library root: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", mf.Path, err)
	}
	if err := utils.CheckPathContained(resolvedRoot, resolvedTarget); err != nil {
		return "", fmt.Errorf("resolved path escapes the library: %w", err)
	}

	return target, nil
}

// checkTrashOutsideLibrary refuses to run when the trash folder sits inside a music
// folder, which would make the next scan re-import everything just deleted.
//
// The comparison is done on both the lexical and the symlink-resolved paths, since a music
// folder reached through a symlink would otherwise look unrelated to the trash.
func checkTrashOutsideLibrary(trashRoot, libraryPath string) error {
	if libraryPath == "" {
		return nil
	}
	libRoot, err := filepath.Abs(libraryPath)
	if err != nil {
		return err
	}

	inside := utils.CheckPathContained(libRoot, trashRoot) == nil
	if !inside {
		// Only compare resolved forms when both actually resolve; a trash folder that does
		// not exist yet is normal on a first delete.
		if resolvedLib, err := filepath.EvalSymlinks(libRoot); err == nil {
			if resolvedTrash, err := filepath.EvalSymlinks(trashRoot); err == nil {
				inside = utils.CheckPathContained(resolvedLib, resolvedTrash) == nil
			} else if resolvedTrash, err := filepath.EvalSymlinks(filepath.Dir(trashRoot)); err == nil {
				inside = utils.CheckPathContained(resolvedLib, filepath.Join(resolvedTrash, filepath.Base(trashRoot))) == nil
			}
		}
	}

	if inside {
		return fmt.Errorf("%w: trash folder %q is inside the music folder %q; set Deletion.TrashFolder to a path outside it",
			ErrUnsafeDeletion, trashRoot, libRoot)
	}
	return nil
}

// plannedMove is one validated file waiting to be moved into the trash batch.
type plannedMove struct {
	mf  model.MediaFile
	src string
	rel string
}

// Indirected so tests can stand in for a full, cross-device trash volume. Neither a
// nearly-full filesystem nor a second device can be staged inside a unit test, and the
// alternative - writing multi-gigabyte files - is not a test anyone should run.
var (
	fsFreeBytes = fsFreeBytesOf
	fsDeviceID  = fsDeviceIDOf
)

// trashFreeSpaceMargin is the headroom checkTrashCapacity insists on leaving free after
// a delete. The trash defaults to <DataFolder>/trash, and that volume also holds
// navidrome.db, its WAL and the artwork cache - running it to the last byte breaks every
// subsequent write even when the copy itself succeeds. 64 MiB is enough for a WAL
// checkpoint and small enough that a normal delete on a modest server is never refused;
// the 5% added on top of it covers per-file block rounding on the destination.
const trashFreeSpaceMargin = 64 << 20

// checkTrashCapacity refuses, before anything moves, a delete that would not fit in the
// trash.
//
// The trash lives under DataFolder, which in the standard Docker layout is a different -
// and much smaller - volume than the music folder. os.Rename then fails with EXDEV and
// moveFile streams the whole file across instead, so deleting a large album can fill the
// volume holding the database and take the server down with it. ErrUnsafeDeletion is the
// right refusal here precisely because it means "nothing has been moved".
//
// Only files that would really be copied are counted: a same-device move is a rename and
// costs no space at all, and refusing it would break the very common single-volume
// install (where deleting is often exactly how the admin is trying to free space).
func checkTrashCapacity(ctx context.Context, trashRoot string, planned []plannedMove) error {
	if len(planned) == 0 {
		return nil
	}
	// The trash folder itself is only created on the first delete, so measure the nearest
	// parent that does exist - it is on the same filesystem by definition.
	base := nearestExistingDir(trashRoot)
	if base == "" {
		return nil
	}
	trashDev, err := fsDeviceID(base)
	if err != nil {
		// No statfs on this platform, or the folder moved under us. This is a safety net,
		// not a gate: skipping it restores the previous behaviour, refusing would break
		// every delete.
		log.Debug(ctx, "Could not identify the trash filesystem, skipping the free space check",
			"trash", base, err)
		return nil
	}

	var needed uint64
	for _, p := range planned {
		if p.mf.Size <= 0 {
			continue
		}
		dev, err := fsDeviceID(p.src)
		if err != nil || dev == trashDev {
			// Same device, so this one is a rename. An error here means the move will fail
			// on its own terms; do not turn it into a capacity refusal.
			continue
		}
		needed += uint64(p.mf.Size)
	}
	if needed == 0 {
		return nil
	}

	free, err := fsFreeBytes(base)
	if err != nil {
		log.Warn(ctx, "Could not read free space on the trash filesystem, skipping the check",
			"trash", base, err)
		return nil
	}

	margin := max(needed/20, uint64(trashFreeSpaceMargin))
	if free < needed+margin {
		return fmt.Errorf("%w: moving these files to the trash would copy %s onto %s, which has only %s free (%s must stay free for the database)",
			ErrUnsafeDeletion, humanize.IBytes(needed), base, humanize.IBytes(free), humanize.IBytes(margin))
	}
	return nil
}

// nearestExistingDir walks up from path to the first directory that already exists,
// since the trash folder is only created on the first delete. Returns "" if nothing on
// the way up is a directory, which on a sane system cannot happen.
func nearestExistingDir(path string) string {
	for {
		if fi, err := os.Stat(path); err == nil && fi.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func trashRoot() (string, error) {
	configured := conf.Server.Deletion.TrashFolder
	if configured == "" {
		return "", errors.New("no trash folder configured")
	}
	return filepath.Abs(configured)
}

// newTrashBatch creates a fresh timestamped folder for one delete request, so files from
// different deletions never overwrite each other.
func newTrashBatch(trashRoot string) (string, error) {
	if err := os.MkdirAll(trashRoot, 0o755); err != nil {
		return "", fmt.Errorf("creating trash folder %q: %w", trashRoot, err)
	}
	stamp := time.Now().Format("2006-01-02T15-04-05")
	for i := range 100 {
		candidate := filepath.Join(trashRoot, stamp)
		if i > 0 {
			candidate = filepath.Join(trashRoot, fmt.Sprintf("%s-%d", stamp, i+1))
		}
		// Mkdir is atomic, so concurrent requests each end up with their own batch.
		err := os.Mkdir(candidate, 0o755)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("creating trash batch folder: %w", err)
		}
	}
	return "", errors.New("could not create a unique trash batch folder")
}

// removeEmptyTree deletes dir and every directory below it, and nothing else.
//
// A batch that failed on its first file is not one empty folder but a nest of them:
// moveFile creates the destination's parent folders before it attempts the move, so
// os.Remove(batch) alone would always fail with ENOTEMPTY and leave the batch behind.
// Every removal here is still a plain os.Remove, which on a directory only succeeds when
// it is empty - one stray file, or anything that is not a directory, stops the prune and
// keeps the whole batch. It is structurally incapable of deleting a file.
func removeEmptyTree(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// IsDir is false for a symlink to a directory, which is what we want: never
		// descend through one.
		if !e.IsDir() {
			return fmt.Errorf("%q is not empty", dir)
		}
		if err := removeEmptyTree(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(dir)
}

type trashEntry struct {
	ID           string `json:"id"`
	LibraryID    int    `json:"libraryId"`
	OriginalPath string `json:"originalPath"`
	TrashPath    string `json:"trashPath"`
}

// writeTrashManifest records where each file came from, so restoring a batch by hand is a
// matter of reading one file instead of guessing at the folder layout.
func writeTrashManifest(batch string, entries []trashEntry) error {
	body, err := json.MarshalIndent(struct {
		DeletedAt time.Time    `json:"deletedAt"`
		Files     []trashEntry `json:"files"`
	}{DeletedAt: time.Now(), Files: entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(batch, "manifest.json"), body, 0o644)
}

// moveFile moves src to dest, falling back to copy-and-remove when the two are on
// different filesystems (the trash folder lives under DataFolder, which is very often a
// different mount from the music folder).
//
// dest must not already exist. Rename would silently replace it, and on a case-folding or
// unicode-normalizing filesystem two distinct library paths can collide inside one batch -
// clobbering a file we just promised to keep.
func moveFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%q already exists in the trash", dest)
	} else if !os.IsNotExist(err) {
		return err
	}

	err := os.Rename(src, dest)
	if err == nil {
		return nil
	}
	// Only a genuine cross-device rename earns the copy fallback. Falling back on *any*
	// error retried a permission problem, a busy file or a source that vanished under us
	// as a second, weaker attempt at the same move - and that second attempt is the one
	// that reads through symlinks. EXDEV arrives wrapped in an *os.LinkError, which
	// errors.Is unwraps.
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	if err := copyFile(src, dest); err != nil {
		return err
	}
	// Only unlink the original once the copy is durable on disk. If the unlink fails the
	// move did not happen, so drop the copy rather than leave the same track sitting in
	// both the library and the trash.
	if err := os.Remove(src); err != nil {
		_ = os.Remove(dest)
		return err
	}
	return nil
}

// copyFile copies src to dest without following a symlink at src.
//
// resolveLibraryFile already rejects symlinks, but that check and this copy are separated
// by the rest of the planning loop. Anyone with write access to the music folder can use
// that window to swap a track for a link to a file the server can read - navidrome.db, a
// mounted secret - and, since the trash is normally on another device, the copy path is
// the one that runs. O_NOFOLLOW closes the window at open time, and the mode is then
// re-checked on the open handle, which is the only form of the check that cannot be raced.
func copyFile(src, dest string) error {
	in, err := os.OpenFile(src, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", src)
	}

	// O_EXCL so a racing writer cannot make us overwrite something.
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return err
	}
	// This is the only remaining copy once the source is unlinked, so make sure it has
	// actually reached the disk before moveFile removes the original.
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return err
	}
	return nil
}
