// Package state records what is installed on disk.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// File is the data-directory state document.
type File struct {
	Extensions map[string]Installed `json:"extensions"`
}

// Installed is one unpacked extension.
type Installed struct {
	Repo         string    `json:"repo"`
	Tag          string    `json:"tag"`
	Asset        string    `json:"asset"`
	Digest       string    `json:"digest,omitempty"`
	RemoteDigest string    `json:"remote_digest,omitempty"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	Version      string    `json:"version,omitempty"`
	Path         string    `json:"path"`
	PathID       string    `json:"path_id,omitempty"`
	CRXID        string    `json:"crx_id,omitempty"`
	CRXPath      string    `json:"crx_path,omitempty"`
	ExternalJSON []string  `json:"external_json,omitempty"`
	InstalledAt  time.Time `json:"installed_at"`
}

// Load reads state. A missing file is empty.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{Extensions: map[string]Installed{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if f.Extensions == nil {
		f.Extensions = map[string]Installed{}
	}
	return &f, nil
}

// Save writes state atomically.
func Save(path string, f *File) error {
	if f.Extensions == nil {
		f.Extensions = map[string]Installed{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
