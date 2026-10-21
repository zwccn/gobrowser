package runtimefetch

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// makeZip 造一个"解压后就是 runtime 目录"的 zip。
func makeZip(t *testing.T, goos string) ([]byte, int64) {
	t.Helper()
	exe := "gobrowser"
	if goos == "windows" {
		exe += ".exe"
	}
	files := map[string]string{
		exe:                  "MZ fake binary",
		"electron.sign":      "sign",
		"lic/demo.lic":       "lic",
		"resources/app.asar": "asar",
		"locales/zh-CN.pak":  "pak",
	}
	p := filepath.Join(t.TempDir(), "rt.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b, int64(len(b))
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// serve 起一个假的发布服务器，记录被请求次数。
func serve(t *testing.T, body []byte, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		_, _ = w.Write(body)
	}))
}

func TestEnsureDownloadsAndCaches(t *testing.T) {
	body, size := makeZip(t, "windows")
	var hits int32
	ts := serve(t, body, &hits)
	defer ts.Close()

	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{File: m.Platforms["windows/amd64"].File, Size: size, SHA256: sum(body)}

	// 用本机平台跑，避免 Ensure 的平台判断与测试机不一致
	opts := Options{Dir: t.TempDir(), BaseURL: ts.URL, Client: ts.Client()}
	dir, err := ensureWith(entry, opts, "windows", "amd64")
	if err != nil {
		t.Fatalf("首次获取失败: %v", err)
	}
	if ok, missing := Valid(dir, "windows"); !ok {
		t.Fatalf("解压后不完整，缺 %v", missing)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("应该只下载一次，实际 %d", got)
	}

	// 第二次：命中缓存，不再请求
	if _, err := ensureWith(entry, opts, "windows", "amd64"); err != nil {
		t.Fatalf("二次获取失败: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("缓存没命中，请求次数变成 %d", got)
	}
}

func TestEnsureRejectsBadSHA(t *testing.T) {
	body, size := makeZip(t, "windows")
	ts := serve(t, body, nil)
	defer ts.Close()

	entry := Entry{File: "rt.zip", Size: size, SHA256: strings.Repeat("0", 64)}
	opts := Options{Dir: t.TempDir(), BaseURL: ts.URL, Client: ts.Client()}
	if _, err := ensureWith(entry, opts, "windows", "amd64"); err == nil {
		t.Fatal("sha256 不符时应该报错")
	} else if !strings.Contains(err.Error(), "校验") {
		t.Fatalf("错误信息没说清校验失败: %v", err)
	}
}

func TestEnsureUsesLocalDir(t *testing.T) {
	body, size := makeZip(t, "windows")
	local := t.TempDir()
	entry := Entry{File: "rt.zip", Size: size, SHA256: sum(body)}
	if err := os.WriteFile(filepath.Join(local, entry.File), body, 0o644); err != nil {
		t.Fatal(err)
	}
	var hits int32
	ts := serve(t, body, &hits)
	defer ts.Close()

	opts := Options{Dir: t.TempDir(), BaseURL: ts.URL, LocalDir: local, Client: ts.Client()}
	if _, err := ensureWith(entry, opts, "windows", "amd64"); err != nil {
		t.Fatalf("本地目录获取失败: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("有本地包时不应该联网，实际请求 %d 次", got)
	}
}

func TestEntryUnknownPlatform(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Entry("plan9", "mips", ""); err == nil {
		t.Fatal("未知平台应该报错")
	}
}

func TestManifestHasCurrentPlatform(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Platforms[Current()]; !ok {
		t.Fatalf("清单里没有当前平台 %s", Current())
	}
	for k, e := range m.Platforms {
		if e.File == "" || e.Size <= 0 || len(e.SHA256) != 64 {
			t.Errorf("%s 的条目不完整: %+v", k, e)
		}
	}
}

// ensureWith 跳过清单里的平台查找，直接注入条目（便于测试任意平台）。
func ensureWith(e Entry, o Options, goos, goarch string) (string, error) {
	root := o.Dir
	dst := filepath.Join(root, "test-"+goos+"-"+goarch)
	if ok, _ := Valid(dst, goos); ok {
		return dst, nil
	}
	zipPath, err := obtainZip(e, o.BaseURL+"/"+e.File, root, o)
	if err != nil {
		return "", err
	}
	if err := extractZip(zipPath, dst, goos); err != nil {
		return "", fmt.Errorf("解压运行时失败: %w", err)
	}
	return dst, nil
}
