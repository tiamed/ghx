package archive

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ghx/internal/crxid"
	"ghx/internal/xerr"
)

const (
	maxFiles         = 20000
	maxUnpackedBytes = 1 << 30
	maxCRXHeader     = 1 << 20
)

// Result is a successful unpack.
type Result struct {
	Version string
	Name    string
	CRXID   string
}

// PackZip builds a zip archive for tests and for nested-archive fixtures.
func PackZip(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetModTime(time.Unix(0, 0))
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unpack extracts a zip or CRX into dest, which must be an empty directory.
// A single top-level folder that contains manifest.json is hoisted.
func Unpack(src, dest string) (Result, error) {
	return unpackDepth(src, dest, 0)
}

func unpackDepth(src, dest string, depth int) (Result, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return Result{}, err
	}
	f, err := os.Open(src)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Result{}, err
	}
	off, crxID, err := payloadOffset(f, info.Size())
	if err != nil {
		return Result{}, err
	}
	zr, err := zip.NewReader(io.NewSectionReader(f, off, info.Size()-off), info.Size()-off)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s is not a zip or crx (%v)", xerr.ErrVerify, filepath.Base(src), err)
	}
	if err := extractZip(zr, dest); err != nil {
		return Result{}, err
	}
	if err := ensureManifestRoot(dest, depth); err != nil {
		return Result{}, err
	}
	ver, name, err := readManifest(filepath.Join(dest, "manifest.json"))
	if err != nil {
		return Result{}, err
	}
	return Result{Version: ver, Name: name, CRXID: crxID}, nil
}

func payloadOffset(r io.ReaderAt, size int64) (int64, string, error) {
	if size < 4 {
		return 0, "", fmt.Errorf("%w: file is too small", xerr.ErrVerify)
	}
	magic := make([]byte, 4)
	if _, err := r.ReadAt(magic, 0); err != nil {
		return 0, "", err
	}
	if string(magic) == "PK\x03\x04" || string(magic) == "PK\x05\x06" || string(magic) == "PK\x07\x08" {
		return 0, "", nil
	}
	if string(magic) != "Cr24" {
		return 0, "", fmt.Errorf("%w: not a zip or CRX file", xerr.ErrVerify)
	}
	if size < 12 {
		return 0, "", fmt.Errorf("%w: truncated CRX header", xerr.ErrVerify)
	}
	prefix := make([]byte, 12)
	if _, err := r.ReadAt(prefix, 0); err != nil {
		return 0, "", err
	}
	ver := binary.LittleEndian.Uint32(prefix[4:8])
	switch ver {
	case 2:
		pubLen := binary.LittleEndian.Uint32(prefix[8:12])
		var sigBuf [4]byte
		if _, err := r.ReadAt(sigBuf[:], 12); err != nil {
			return 0, "", err
		}
		sigLen := binary.LittleEndian.Uint32(sigBuf[:])
		if pubLen == 0 || pubLen > maxCRXHeader || sigLen > maxCRXHeader {
			return 0, "", fmt.Errorf("%w: CRX2 key length is invalid", xerr.ErrVerify)
		}
		off := int64(16 + pubLen + sigLen)
		if off >= size {
			return 0, "", fmt.Errorf("%w: CRX2 header overruns the file", xerr.ErrVerify)
		}
		pub := make([]byte, pubLen)
		if _, err := r.ReadAt(pub, 16); err != nil {
			return 0, "", err
		}
		return off, crxid.Generate(pub), nil
	case 3:
		n := binary.LittleEndian.Uint32(prefix[8:12])
		if n == 0 || n > maxCRXHeader {
			return 0, "", fmt.Errorf("%w: CRX3 header length %d is invalid", xerr.ErrVerify, n)
		}
		off := int64(12 + n)
		if off >= size {
			return 0, "", fmt.Errorf("%w: CRX3 header overruns the file", xerr.ErrVerify)
		}
		header := make([]byte, n)
		if _, err := r.ReadAt(header, 12); err != nil {
			return 0, "", err
		}
		id, err := idFromCRX3(header)
		if err != nil {
			return 0, "", err
		}
		return off, id, nil
	default:
		return 0, "", fmt.Errorf("%w: unsupported CRX version %d", xerr.ErrVerify, ver)
	}
}

func extractZip(zr *zip.Reader, dest string) error {
	if len(zr.File) > maxFiles {
		return fmt.Errorf("%w: archive has too many files", xerr.ErrVerify)
	}
	var total int64
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 {
			return fmt.Errorf("%w: encrypted zip entry %s", xerr.ErrVerify, f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink zip entry %s", xerr.ErrVerify, f.Name)
		}
		name, err := safeName(f.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !within(dest, target) {
			return fmt.Errorf("%w: zip entry escapes destination: %s", xerr.ErrVerify, f.Name)
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.UncompressedSize64 > maxUnpackedBytes {
			return fmt.Errorf("%w: %s is too large", xerr.ErrVerify, f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > maxUnpackedBytes {
			return fmt.Errorf("%w: uncompressed archive is too large", xerr.ErrVerify)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		perm := f.Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxUnpackedBytes+1))
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%w: absolute zip path %s", xerr.ErrVerify, name)
	}
	var parts []string
	for _, p := range strings.Split(name, "/") {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			return "", fmt.Errorf("%w: zip path escapes: %s", xerr.ErrVerify, name)
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "/"), nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func ensureManifestRoot(dest string, depth int) error {
	if fileExists(filepath.Join(dest, "manifest.json")) {
		return nil
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	var dirs, files []os.DirEntry
	for _, e := range entries {
		if e.Name() == "__MACOSX" || e.Name() == ".DS_Store" {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	if len(dirs) == 1 && len(files) == 0 {
		sub := filepath.Join(dest, dirs[0].Name())
		if fileExists(filepath.Join(sub, "manifest.json")) {
			return hoist(sub, dest)
		}
	}
	if depth < 1 && len(files) == 1 && len(dirs) == 0 {
		low := strings.ToLower(files[0].Name())
		if strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".crx") || strings.HasSuffix(low, ".crx3") {
			nested := filepath.Join(dest, files[0].Name())
			parent := filepath.Dir(dest)
			tmp, err := os.MkdirTemp(parent, "nested-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			if _, err := unpackDepth(nested, tmp, depth+1); err != nil {
				return err
			}
			if err := os.RemoveAll(dest); err != nil {
				return err
			}
			return os.Rename(tmp, dest)
		}
	}
	return fmt.Errorf("%w: archive has no manifest.json", xerr.ErrVerify)
}

func hoist(sub, dest string) error {
	entries, err := os.ReadDir(sub)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.Rename(filepath.Join(sub, e.Name()), filepath.Join(dest, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(sub)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func readManifest(p string) (version, name string, err error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", "", fmt.Errorf("%w: manifest.json: %v", xerr.ErrVerify, err)
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	var raw struct {
		Name    string          `json:"name"`
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", "", fmt.Errorf("%w: manifest.json: %v", xerr.ErrVerify, err)
	}
	ver := strings.Trim(string(raw.Version), `"`)
	ver = strings.TrimSpace(ver)
	if ver == "" || ver == "null" {
		return "", "", fmt.Errorf("%w: manifest.json has no version", xerr.ErrVerify)
	}
	return ver, raw.Name, nil
}

// idFromCRX3 reads the Chromium CrxFileHeader and returns the extension id.
func idFromCRX3(header []byte) (string, error) {
	fields, err := parseProto(header)
	if err != nil {
		return "", fmt.Errorf("%w: CRX3 header: %v", xerr.ErrVerify, err)
	}
	var crxID []byte
	var pub []byte
	for _, f := range fields {
		switch f.num {
		case 10000:
			inner, err := parseProto(f.data)
			if err != nil {
				return "", err
			}
			for _, in := range inner {
				if in.num == 1 && len(in.data) >= 16 {
					crxID = in.data[:16]
				}
			}
		case 2, 3: // rsa, ecdsa proofs
			if pub != nil {
				continue
			}
			inner, err := parseProto(f.data)
			if err != nil {
				return "", err
			}
			for _, in := range inner {
				if in.num == 1 && len(in.data) > 0 {
					pub = in.data
				}
			}
		}
	}
	if pub != nil {
		id := crxid.Generate(pub)
		if crxID != nil {
			fromHeader := crxid.FromHash(crxID)
			if fromHeader != id {
				return "", fmt.Errorf("%w: CRX id does not match the public key", xerr.ErrVerify)
			}
		}
		return id, nil
	}
	if crxID != nil {
		return crxid.FromHash(crxID), nil
	}
	return "", fmt.Errorf("%w: CRX3 header has no extension id", xerr.ErrVerify)
}

type protoField struct {
	num  int
	data []byte
}

func parseProto(b []byte) ([]protoField, error) {
	var out []protoField
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errors.New("bad varint")
		}
		b = b[n:]
		field := int(tag >> 3)
		wire := int(tag & 7)
		switch wire {
		case 0:
			_, n = binary.Uvarint(b)
			if n <= 0 {
				return nil, errors.New("bad varint")
			}
			b = b[n:]
		case 1:
			if len(b) < 8 {
				return nil, errors.New("truncated")
			}
			b = b[8:]
		case 5:
			if len(b) < 4 {
				return nil, errors.New("truncated")
			}
			b = b[4:]
		case 2:
			ln, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errors.New("bad length")
			}
			b = b[n:]
			if uint64(len(b)) < ln {
				return nil, errors.New("truncated length-delimited field")
			}
			out = append(out, protoField{num: field, data: append([]byte(nil), b[:ln]...)})
			b = b[ln:]
		default:
			return nil, fmt.Errorf("unsupported wire type %d", wire)
		}
	}
	return out, nil
}
