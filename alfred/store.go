package alfred

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Store keeps files in one directory, the workflow's cache or data folder.
type Store struct{ dir string }

// NewStore returns a Store for dir. The directory is created on the first write.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Path returns the file path for key.
func (s *Store) Path(key string) string { return filepath.Join(s.dir, key) }

func (s *Store) Exists(key string) bool {
	_, err := os.Stat(s.Path(key))
	return err == nil
}

// Age returns how long ago key was written.
func (s *Store) Age(key string) (time.Duration, error) {
	fi, err := os.Stat(s.Path(key))
	if err != nil {
		return 0, err
	}
	return time.Since(fi.ModTime()), nil
}

// Expired reports whether key is missing or older than maxAge.
func (s *Store) Expired(key string, maxAge time.Duration) bool {
	age, err := s.Age(key)
	return err != nil || age > maxAge
}

func (s *Store) Load(key string) ([]byte, error) { return os.ReadFile(s.Path(key)) }

func (s *Store) LoadJSON(key string, v any) error {
	data, err := s.Load(key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode %s: %w", key, err)
	}
	return nil
}

// LoadJSONIfExists is LoadJSON, but a missing key leaves v as it is.
func (s *Store) LoadJSONIfExists(key string, v any) error {
	err := s.LoadJSON(key, v)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Store writes data to key through a temp file and a rename, so a reader in
// another process never sees a partly written file.
func (s *Store) Store(key string, data []byte) error {
	path := s.Path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // gone after the rename; this only cleans up on failure
	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck,gosec // the write error is the one to report
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Store) StoreJSON(key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	return s.Store(key, data)
}

// Delete removes key. A missing key is not an error.
func (s *Store) Delete(key string) error {
	err := os.Remove(s.Path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
