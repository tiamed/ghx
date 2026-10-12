// Package crxid computes Chromium extension ids.
//
// The algorithm matches components/crx_file/id_util.cc: SHA-256, the first
// 16 bytes, each nibble mapped onto 'a'–'p'. Packed CRX ids hash the public
// key. Unpacked ids hash the absolute directory path (UTF-8 on Linux, UTF-16LE
// on Windows, with the drive letter upper-cased).
package crxid

import (
	"crypto/sha256"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Generate returns the extension id for an arbitrary byte string (a public key
// or a path encoded the way Chromium hashes it).
func Generate(data []byte) string {
	sum := sha256.Sum256(data)
	return FromHash(sum[:16])
}

// FromHash maps the first 16 bytes of a SHA-256 digest to an extension id.
func FromHash(hash []byte) string {
	if len(hash) > 16 {
		hash = hash[:16]
	}
	var b strings.Builder
	b.Grow(len(hash) * 2)
	for _, by := range hash {
		b.WriteByte('a' + (by >> 4))
		b.WriteByte('a' + (by & 0x0f))
	}
	return b.String()
}

// Valid reports whether id is 32 characters in the a–p alphabet.
func Valid(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 'a' || id[i] > 'p' {
			return false
		}
	}
	return true
}

// ForPath returns the unpacked extension id Chrome derives from an absolute path.
// goos is "windows" or anything else (treated as a POSIX path hashed as UTF-8).
func ForPath(goos, absPath string) string {
	absPath = filepath.Clean(absPath)
	if goos == "windows" {
		absPath = upperDrive(absPath)
		return Generate(utf16LE(absPath))
	}
	return Generate([]byte(absPath))
}

func upperDrive(path string) string {
	if len(path) >= 2 && path[0] >= 'a' && path[0] <= 'z' && path[1] == ':' {
		return strings.ToUpper(path[:1]) + path[1:]
	}
	return path
}

func utf16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return b
}
