package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"ghx/internal/crxid"
	"ghx/internal/xerr"
)

func manifestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	b, err := PackZip(files)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "ext.zip")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnpackRootManifest(t *testing.T) {
	src := manifestZip(t, map[string]string{
		"manifest.json": `{"name":"Collectab","version":"0.2.18","manifest_version":3}`,
		"app.js":        "console.log(1)",
	})
	dest := filepath.Join(t.TempDir(), "out")
	res, err := Unpack(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "0.2.18" || res.Name != "Collectab" {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dest, "app.js")); err != nil {
		t.Fatal(err)
	}
}

func TestUnpackHoistsSingleDirectory(t *testing.T) {
	src := manifestZip(t, map[string]string{
		"extension/manifest.json": `{"name":"Nested","version":"1.0.0","manifest_version":3}`,
		"extension/bg.js":         "1",
	})
	dest := filepath.Join(t.TempDir(), "out")
	res, err := Unpack(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "1.0.0" {
		t.Fatal(res.Version)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func TestZipSlip(t *testing.T) {
	src := manifestZip(t, map[string]string{
		"../evil.txt": "nope",
	})
	_, err := Unpack(src, filepath.Join(t.TempDir(), "out"))
	if xerr.ExitCode(err) != 4 {
		t.Fatal(err)
	}
}

func TestRejectSymlink(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("manifest.json")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "s.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Unpack(p, filepath.Join(t.TempDir(), "out"))
	if err == nil || xerr.ExitCode(err) != 4 {
		t.Fatal(err)
	}
}

func TestCRX3RoundTrip(t *testing.T) {
	pub := []byte{
		0x30, 0x81, 0x9f, 0x30, 0x0d, 0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7,
		0x0d, 0x01, 0x01, 0x01, 0x05, 0x00, 0x03, 0x81, 0x8d, 0x00, 0x30, 0x81,
		0x89, 0x02, 0x81, 0x81, 0x00, 0xb8, 0x7f, 0x2b, 0x20, 0xdc, 0x7c, 0x9b,
		0x0c, 0xdc, 0x51, 0x61, 0x99, 0x0d, 0x36, 0x0f, 0xd4, 0x66, 0x88, 0x08,
		0x55, 0x84, 0xd5, 0x3a, 0xbf, 0x2b, 0xa4, 0x64, 0x85, 0x7b, 0x0c, 0x04,
		0x13, 0x3f, 0x8d, 0xf4, 0xbc, 0x38, 0x0d, 0x49, 0xfe, 0x6b, 0xc4, 0x5a,
		0xb0, 0x40, 0x53, 0x3a, 0xd7, 0x66, 0x09, 0x0f, 0x9e, 0x36, 0x74, 0x30,
		0xda, 0x8a, 0x31, 0x4f, 0x1f, 0x14, 0x50, 0xd7, 0xc7, 0x20, 0x94, 0x17,
		0xde, 0x4e, 0xb9, 0x57, 0x5e, 0x7e, 0x0a, 0xe5, 0xb2, 0x65, 0x7a, 0x89,
		0x4e, 0xb6, 0x47, 0xff, 0x1c, 0xbd, 0xb7, 0x38, 0x13, 0xaf, 0x47, 0x85,
		0x84, 0x32, 0x33, 0xf3, 0x17, 0x49, 0xbf, 0xe9, 0x96, 0xd0, 0xd6, 0x14,
		0x6f, 0x13, 0x8d, 0xc5, 0xfc, 0x2c, 0x72, 0xba, 0xac, 0xea, 0x7e, 0x18,
		0x53, 0x56, 0xa6, 0x83, 0xa2, 0xce, 0x93, 0x93, 0xe7, 0x1f, 0x0f, 0xe6,
		0x0f, 0x02, 0x03, 0x01, 0x00, 0x01,
	}
	sum := sha256.Sum256(pub)
	signed := protoBytes(1, sum[:16])
	proof := protoBytes(1, pub)
	header := append(protoBytes(10000, signed), protoBytes(2, proof)...)
	zipBytes, err := PackZip(map[string]string{
		"manifest.json": `{"name":"CRX","version":"9.9.9","manifest_version":3}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.WriteString("Cr24")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(3))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(header)))
	buf.Write(header)
	buf.Write(zipBytes)
	p := filepath.Join(t.TempDir(), "ext.crx")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Unpack(p, filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "9.9.9" || res.CRXID != crxid.Generate(pub) {
		t.Fatalf("%+v", res)
	}
	if res.CRXID != "melddjfinppjdikinhbgehiennejpfhp" {
		t.Fatal(res.CRXID)
	}
}

func TestCRX2(t *testing.T) {
	pub := []byte("test-public-key-bytes")
	zipBytes, err := PackZip(map[string]string{
		"manifest.json": `{"name":"C","version":"1","manifest_version":3}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	sig := []byte("sig")
	var buf bytes.Buffer
	buf.WriteString("Cr24")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pub)))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(sig)))
	buf.Write(pub)
	buf.Write(sig)
	buf.Write(zipBytes)
	p := filepath.Join(t.TempDir(), "ext.crx")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Unpack(p, filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	if res.CRXID != crxid.Generate(pub) || res.Version != "1" {
		t.Fatalf("%+v", res)
	}
}

func protoBytes(field int, data []byte) []byte {
	tag := uint64(field<<3 | 2)
	var buf []byte
	buf = binary.AppendUvarint(buf, tag)
	buf = binary.AppendUvarint(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

func TestBOMManifest(t *testing.T) {
	body := "\uFEFF" + `{"name":"B","version":"2.0","manifest_version":3}`
	// PackZip writes the string as UTF-8, so the BOM rune becomes EF BB BF. Good.
	src := manifestZip(t, map[string]string{"manifest.json": body})
	res, err := Unpack(src, filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "2.0" {
		t.Fatal(res.Version)
	}
}
