package payloadfmt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "app.exe")
	zipf := filepath.Join(dir, "payload.zip")

	exeBody := bytes.Repeat([]byte{0x90}, 1024) // 假装是原 exe
	if err := os.WriteFile(exe, exeBody, 0o755); err != nil {
		t.Fatal(err)
	}
	zipBody := []byte("PK\x03\x04fake-zip-content")
	if err := os.WriteFile(zipf, zipBody, 0o644); err != nil {
		t.Fatal(err)
	}

	tr, err := Append(exe, zipf)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Offset != int64(len(exeBody)) {
		t.Fatalf("offset 应为 %d，实际 %d", len(exeBody), tr.Offset)
	}
	if tr.Size != int64(len(zipBody)) {
		t.Fatalf("size 应为 %d，实际 %d", len(zipBody), tr.Size)
	}

	got, ok := Read(exe)
	if !ok {
		t.Fatal("Read 应返回 ok")
	}
	if got.Offset != tr.Offset || got.Size != tr.Size || !bytes.Equal(got.Sum, tr.Sum) {
		t.Fatalf("读回不一致: %+v vs %+v", got, tr)
	}

	// 载荷内容确实在文件里
	full, _ := os.ReadFile(exe)
	if !bytes.Equal(full[got.Offset:got.Offset+got.Size], zipBody) {
		t.Fatal("载荷内容不匹配")
	}
}

func TestReadWithoutPayload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.exe")
	if err := os.WriteFile(p, bytes.Repeat([]byte{0}, 64), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := Read(p); ok {
		t.Fatal("没有载荷时不应返回 ok")
	}
}

func TestReadTruncated(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "app.exe")
	zipf := filepath.Join(dir, "p.zip")
	os.WriteFile(exe, bytes.Repeat([]byte{1}, 128), 0o755)
	os.WriteFile(zipf, bytes.Repeat([]byte{2}, 32), 0o644)
	if _, err := Append(exe, zipf); err != nil {
		t.Fatal(err)
	}
	// 砍掉尾部 10 字节 → trailer 不完整，应判为没有载荷
	b, _ := os.ReadFile(exe)
	os.WriteFile(exe, b[:len(b)-10], 0o755)
	if _, ok := Read(exe); ok {
		t.Fatal("截断后不应返回 ok")
	}
}
