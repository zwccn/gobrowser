package asar

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteLayout(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	html := "<h1>hi</h1>"
	js := "console.log(1)"
	os.WriteFile(filepath.Join(src, "index.html"), []byte(html), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "main.js"), []byte(js), 0o644)

	out := filepath.Join(t.TempDir(), "app.asar")
	if err := Write(src, out); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 12 {
		t.Fatalf("文件太小: %d", len(raw))
	}
	// 前缀：[u32 4][u32 headerLen]
	if binary.LittleEndian.Uint32(raw[0:4]) != 4 {
		t.Fatalf("首个 u32 必须是 4，实际 %d", binary.LittleEndian.Uint32(raw[0:4]))
	}
	headerLen := binary.LittleEndian.Uint32(raw[4:8])
	if int(headerLen)+8+len(html)+len(js) != len(raw) {
		t.Fatalf("文件长度与头部不符: headerLen=%d total=%d", headerLen, len(raw))
	}

	// 解析 header pickle：[u32 payloadLen][u32 jsonLen][json...]
	hp := raw[8 : 8+headerLen]
	payloadLen := binary.LittleEndian.Uint32(hp[0:4])
	jsonLen := binary.LittleEndian.Uint32(hp[4:8])
	if int(payloadLen) != int(headerLen)-4 {
		t.Fatalf("payloadLen=%d 应为 %d", payloadLen, headerLen-4)
	}
	var root struct {
		Files map[string]struct {
			Size   int64  `json:"size"`
			Offset string `json:"offset"`
			Files  map[string]struct {
				Size   int64  `json:"size"`
				Offset string `json:"offset"`
			} `json:"files"`
		} `json:"files"`
	}
	if err := json.Unmarshal(hp[8:8+jsonLen], &root); err != nil {
		t.Fatalf("header JSON 解析失败: %v", err)
	}
	if f, ok := root.Files["index.html"]; !ok || f.Size != int64(len(html)) || f.Offset != "0" {
		t.Fatalf("index.html 条目错误: %+v", root.Files)
	}
	sub, ok := root.Files["sub"]
	if !ok {
		t.Fatal("缺少 sub 目录")
	}
	if m, ok := sub.Files["main.js"]; !ok || m.Offset != strconv.Itoa(len(html)) {
		t.Fatalf("sub/main.js 条目错误: %+v", sub.Files)
	}
}
