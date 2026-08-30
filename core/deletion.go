package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
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
	type plannedMove struct {
		mf  model.MediaFile
		src string
		rel string
	}
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
		if err := checkContained(batch, dest); err != nil {
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
	target, err := filepath.Abs(filepath.Join(mf.LibraryPath, mf.Path))
	if err != nil {
		return "", err
	}
	if err := checkContained(root, target); err != nil {
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
	if err := checkContained(resolvedRoot, resolvedTarget); err != nil {
		return "", fmt.Errorf("resolved path escapes the library: %w", err)
	}

	return target, nil
}

// checkContained fails unless child is root itself or sits somewhere beneath it.
func checkContained(root, child string) error {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside %q", child, root)
	}
	return nil
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

	inside := checkContained(libRoot, trashRoot) == nil
	if !inside {
		// Only compare resolved forms when both actually resolve; a trash folder that does
		// not exist yet is normal on a first delete.
		if resolvedLib, err := filepath.EvalSymlinks(libRoot); err == nil {
			if resolvedTrash, err := filepath.EvalSymlinks(trashRoot); err == nil {
				inside = checkContained(resolvedLib, resolvedTrash) == nil
			} else if resolvedTrash, err := filepath.EvalSymlinks(filepath.Dir(trashRoot)); err == nil {
				inside = checkContained(resolvedLib, filepath.Join(resolvedTrash, filepath.Base(trashRoot))) == nil
			}
		}
	}

	if inside {
		return fmt.Errorf("%w: trash folder %q is inside the music folder %q; set Deletion.TrashFolder to a path outside it",
			ErrUnsafeDeletion, trashRoot, libRoot)
	}
	return nil
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

	if err := os.Rename(src, dest); err == nil {
		return nil
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

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
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
