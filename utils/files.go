package utils

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/navidrome/navidrome/model/id"
)

var cleanFileNameRe = regexp.MustCompile(`[^a-z0-9_-]`)

func TempFileName(prefix, suffix string) string {
	return filepath.Join(os.TempDir(), prefix+id.NewRandom()+suffix)
}

func BaseName(filePath string) string {
	p := path.Base(filePath)
	return strings.TrimSuffix(p, path.Ext(p))
}

// CleanFileName produces a filesystem-safe, human-readable version of a name.
// It lowercases, replaces spaces with underscores, strips non-alphanumeric
// characters (except underscore and hyphen), and truncates to 50 characters.
func CleanFileName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "_")
	s = cleanFileNameRe.ReplaceAllString(s, "")
	if len(s) > 50 {
		s = s[:50]
	}
	s = strings.TrimRight(s, "_-")
	return s
}

// FileExists checks if a file or directory exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// CheckPathContained fails unless child is root itself or sits somewhere beneath it.
//
// This is the single implementation of the "never leave the library folder" boundary:
// both the lyrics sidecar writer and the media file deleter call it, so a hardening
// applied here cannot land on one path and miss the other. The comparison is purely
// lexical, so callers that also care about symlinks must resolve both sides themselves
// and check again.
func CheckPathContained(root, child string) error {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside %q", child, root)
	}
	return nil
}
