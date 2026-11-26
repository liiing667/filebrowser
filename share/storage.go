package share

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
	"time"

	fberrors "github.com/filebrowser/filebrowser/v2/errors"
)

// StorageBackend is the interface to implement for a share storage.
type StorageBackend interface {
	All() ([]*Link, error)
	FindByUserID(id uint) ([]*Link, error)
	GetByHash(hash string) (*Link, error)
	GetPermanent(path string, id uint) (*Link, error)
	Gets(path string, id uint) ([]*Link, error)
	Save(s *Link) error
	Delete(hash string) error
	DeleteWithPathPrefix(path string, userID uint) error
}

// Storage is a storage.
type Storage struct {
	back StorageBackend
}

// NewStorage creates a share links storage from a backend.
func NewStorage(back StorageBackend) *Storage {
	return &Storage{back: back}
}

// All wraps a StorageBackend.All.
func (s *Storage) All() ([]*Link, error) {
	links, err := s.back.All()

	if err != nil {
		return nil, err
	}

	for i, link := range links {
		if link.Expire != 0 && link.Expire <= time.Now().Unix() {
			if err := s.Delete(link.Hash); err != nil {
				return nil, err
			}
			links = append(links[:i], links[i+1:]...)
		}
	}

	return links, nil
}

// FindByUserID wraps a StorageBackend.FindByUserID.
func (s *Storage) FindByUserID(id uint) ([]*Link, error) {
	links, err := s.back.FindByUserID(id)

	if err != nil {
		return nil, err
	}

	for i, link := range links {
		if link.Expire != 0 && link.Expire <= time.Now().Unix() {
			if err := s.Delete(link.Hash); err != nil {
				return nil, err
			}
			links = append(links[:i], links[i+1:]...)
		}
	}

	return links, nil
}

// GetByHash wraps a StorageBackend.GetByHash.
func (s *Storage) GetByHash(hash string) (*Link, error) {
	link, err := s.back.GetByHash(hash)
	if err != nil {
		return nil, err
	}

	if link.Expire != 0 && link.Expire <= time.Now().Unix() {
		if err := s.Delete(link.Hash); err != nil {
			return nil, err
		}
		return nil, fberrors.ErrNotExist
	}

	return link, nil
}

// GetPermanent wraps a StorageBackend.GetPermanent
func (s *Storage) GetPermanent(path string, id uint) (*Link, error) {
	return s.back.GetPermanent(path, id)
}

// Gets wraps a StorageBackend.Gets
func (s *Storage) Gets(path string, id uint) ([]*Link, error) {
	links, err := s.back.Gets(path, id)

	if err != nil {
		return nil, err
	}

	for i, link := range links {
		if link.Expire != 0 && link.Expire <= time.Now().Unix() {
			if err := s.Delete(link.Hash); err != nil {
				return nil, err
			}
			links = append(links[:i], links[i+1:]...)
		}
	}

	return links, nil
}

// Save wraps a StorageBackend.Save
func (s *Storage) Save(l *Link) error {
	return s.back.Save(l)
}

// Delete wraps a StorageBackend.Delete
func (s *Storage) Delete(hash string) error {
	return s.back.Delete(hash)
}

func (s *Storage) DeleteWithPathPrefix(path string, userID uint) error {
	return s.back.DeleteWithPathPrefix(path, userID)
}

// MovePathPrefix rewrites links owned by userID whose stored path is oldPath
// or a descendant of it as though that file or directory had been renamed to
// newPath. Share hashes stay stable, so an already distributed link keeps
// pointing at the same object after a rename instead of retaining a stale
// scope-relative path.
//
// Links owned by other users are never modified. A missing share collection is
// not an error.
func (s *Storage) MovePathPrefix(oldPath, newPath string, userID uint) error {
	return s.MovePathPrefixWithFold(oldPath, newPath, userID, false)
}

// MovePathPrefixWithFold is MovePathPrefix with optional case-insensitive path
// matching for case-insensitive filesystems.
func (s *Storage) MovePathPrefixWithFold(oldPath, newPath string, userID uint, fold bool) error {
	oldPath = canonicalSharePath(oldPath)
	newPath = canonicalSharePath(newPath)
	if oldPath == newPath {
		return nil
	}

	links, err := s.FindByUserID(userID)
	if errors.Is(err, fberrors.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var errs []error
	for _, link := range links {
		moved, ok := relocateSharePathFold(link.Path, oldPath, newPath, fold)
		if !ok {
			continue
		}

		link.Path = moved
		if err := s.Save(link); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func canonicalSharePath(p string) string {
	// Share paths are virtual "/"-separated paths. On Windows a legacy record
	// or host-derived caller may still contain "\\"; normalize it before
	// comparing prefixes, matching the HTTP layer's canonical form.
	p = filepath.ToSlash(p)
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	return path.Clean(p)
}

func relocateSharePath(stored, oldPath, newPath string) (string, bool) {
	return relocateSharePathFold(stored, oldPath, newPath, false)
}

func relocateSharePathFold(stored, oldPath, newPath string, fold bool) (string, bool) {
	p := canonicalSharePath(stored)
	oldPath = canonicalSharePath(oldPath)
	newPath = canonicalSharePath(newPath)

	equal := p == oldPath
	hasPrefix := strings.HasPrefix(p, oldPath+"/")
	if fold {
		equal = strings.EqualFold(p, oldPath)
		lowerP := strings.ToLower(p)
		lowerPrefix := strings.ToLower(oldPath) + "/"
		hasPrefix = strings.HasPrefix(lowerP, lowerPrefix)
	}

	if equal {
		return newPath, true
	}

	if oldPath == "/" {
		if p == "/" {
			return newPath, true
		}
		return path.Join(newPath, p), true
	}

	if hasPrefix {
		return newPath + p[len(oldPath):], true
	}

	return stored, false
}
