package runtimefetch

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Options 控制一次获取过程。零值即可用：默认缓存到用户目录、走 HTTPS、不打印任何东西。
type Options struct {
	Dir      string // 缓存根，默认 CacheRoot()
	BaseURL  string // 覆盖清单里的下载前缀（镜像）：GOBROWSER_RUNTIME_BASE
	LocalDir string // 本地 zip 目录：里面有就直接用，不联网（GOBROWSER_RUNTIME_DIR）
	Progress func(done, total int64)
	Logf     func(format string, args ...any)
	Client   *http.Client
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Ensure 返回一个可以直接当作 runtime\ 使用的目录；没有就下载并解压。
// 同一版本 + 平台只下载一次，之后直接命中缓存。
func Ensure(goos, goarch string, o Options) (string, error) {
	m, err := Load()
	if err != nil {
		return "", err
	}
	entry, url, err := m.Entry(goos, goarch, o.BaseURL)
	if err != nil {
		return "", err
	}

	root := o.Dir
	if root == "" {
		root = CacheRoot()
	}
	dst := filepath.Join(root, m.Version+"-"+strings.ReplaceAll(Key(goos, goarch), "/", "-"))
	if ok, _ := Valid(dst, goos); ok {
		o.logf("运行时已在缓存: %s", dst)
		return dst, nil
	}

	zipPath, err := obtainZip(entry, url, root, o)
	if err != nil {
		return "", err
	}
	if err := extractZip(zipPath, dst, goos); err != nil {
		return "", fmt.Errorf("解压运行时失败: %w", err)
	}
	o.logf("运行时就绪: %s", dst)
	return dst, nil
}

// Valid 判断 dir 是不是一份完整的运行时（缺任何关键项都算无效）。
func Valid(dir, goos string) (bool, []string) {
	exe := "gobrowser"
	if goos == "windows" {
		exe += ".exe"
	}
	required := []string{exe, "electron.sign", filepath.Join("resources", "app.asar"), "lic"}
	var missing []string
	for _, rel := range required {
		fi, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || (fi.IsDir() && rel != "lic") {
			missing = append(missing, rel)
		}
	}
	return len(missing) == 0, missing
}

// obtainZip 先看本地目录，再决定是否下载；返回可用的 zip 路径。
func obtainZip(e Entry, url, root string, o Options) (string, error) {
	if o.LocalDir != "" {
		p := filepath.Join(o.LocalDir, e.File)
		if ok, err := checkZip(p, e); ok {
			o.logf("使用本地运行时包: %s", p)
			return p, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	zipDir := filepath.Join(root, "_zips")
	if err := os.MkdirAll(zipDir, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(zipDir, e.File)
	if ok, _ := checkZip(zipPath, e); ok {
		return zipPath, nil
	}

	o.logf("下载运行时: %s", url)
	if err := download(url, zipPath, e, o); err != nil {
		return "", err
	}
	if ok, err := checkZip(zipPath, e); !ok {
		return "", err
	}
	return zipPath, nil
}

func checkZip(p string, e Entry) (bool, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	if fi.IsDir() {
		return false, fmt.Errorf("%s 是目录，不是 zip", p)
	}
	if e.Size > 0 && fi.Size() != e.Size {
		return false, fmt.Errorf("%s 大小不符（%d，期望 %d），已删除重下", p, fi.Size(), e.Size)
	}
	if e.SHA256 == "" {
		return true, nil
	}
	sum, err := fileSHA256(p)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(sum, e.SHA256) {
		_ = os.Remove(p)
		return false, fmt.Errorf("%s 校验失败（sha256 %s，期望 %s），已删除", p, sum, e.SHA256)
	}
	return true, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func download(url, dst string, e Entry, o Options) error {
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 0} // 大文件不设总超时，靠连接级超时兜底
	}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败（可用 GOBROWSER_RUNTIME_BASE 换镜像，或 GOBROWSER_RUNTIME_DIR 指向本地目录）: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s 返回 %d", url, resp.StatusCode)
	}

	tmp := dst + ".part"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	total := e.Size
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	var done int64
	buf := make([]byte, 256*1024)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				_ = os.Remove(tmp)
				return werr
			}
			done += int64(n)
			if o.Progress != nil && (time.Since(last) > 200*time.Millisecond || (total > 0 && done == total)) {
				o.Progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("下载中断: %w", rerr)
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// extractZip 把 zip 解到 dst（先解到 .tmp 再整体改名，避免半成品被当成有效缓存）。
func extractZip(zipPath, dst, goos string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	tmp := dst + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	root := filepath.Clean(tmp)
	for _, f := range zr.File {
		target := filepath.Join(tmp, filepath.FromSlash(f.Name))
		if !within(root, target) {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("包内路径越界: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				_ = os.RemoveAll(tmp)
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		rc, err := f.Open()
		if err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			_ = os.RemoveAll(tmp)
			return err
		}
		_, cerr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if cerr != nil {
			_ = os.RemoveAll(tmp)
			return cerr
		}
		_ = os.Chmod(target, f.Mode()) // Linux 下 gobrowser / chrome-sandbox 需要可执行位
	}

	if ok, missing := Valid(tmp, goos); !ok {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("包内容不完整，缺: %s", strings.Join(missing, ", "))
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// PlatformKey 供外部（工具）做平台名到清单键的映射复用。
func PlatformKey(goos, goarch string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return Key(goos, goarch)
}
