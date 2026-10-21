package gobrowser

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zwccn/gobrowser/internal/payloadfmt"
)

// ErrNoPayload 表示可执行文件里没有附带运行时。
var ErrNoPayload = errors.New("可执行文件里没有附带运行时")

// Payload 指向附加在可执行文件尾部的运行时压缩包（格式见 internal/payloadfmt）。
type Payload struct {
	Offset int64
	Size   int64
	Sum    string
}

// HasPayload 判断当前 exe 里有没有附带的运行时。
func HasPayload() bool {
	_, err := SelfPayload()
	return err == nil
}

// SelfPayload 解析当前可执行文件尾部。
func SelfPayload() (*Payload, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	tr, ok := payloadfmt.Read(exe)
	if !ok {
		return nil, ErrNoPayload
	}
	return &Payload{Offset: tr.Offset, Size: tr.Size, Sum: fmt.Sprintf("%x", tr.Sum)}, nil
}

// Extract 把载荷解压到 dst。
func (p *Payload) Extract(dst string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer f.Close()

	zr, err := zip.NewReader(io.NewSectionReader(f, p.Offset, p.Size), p.Size)
	if err != nil {
		return fmt.Errorf("载荷不是合法 zip: %w", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	root := filepath.Clean(dst)
	for _, zf := range zr.File {
		target := filepath.Join(dst, filepath.FromSlash(zf.Name))
		if !pathWithin(root, target) {
			return fmt.Errorf("载荷内路径越界: %s", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		_ = os.Chmod(target, zf.Mode()) // 保留可执行位（Linux 的 gobrowser/chrome-sandbox 需要）
	}
	return nil
}

// PrepareRuntime 决定"运行时从哪来"：
//
//  1. exe 同级已有完整 runtime → 返回 ""，让 Start 走默认发现逻辑
//  2. exe 里带了载荷 → 首次解压到用户缓存目录，之后按摘要复用
//  3. 都没有 → 返回 ErrNoPayload / 其它错误
func PrepareRuntime(appID string) (string, error) {
	if appID == "" {
		appID = "gobrowser"
	}
	if exe, err := os.Executable(); err == nil {
		if _, err := FindRuntimeAt(filepath.Dir(exe), true); err == nil {
			return "", nil
		}
	}

	p, err := SelfPayload()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(runtimeCacheRoot(appID), "runtime-"+p.Sum[:12])
	marker := filepath.Join(dir, ".payload")
	if b, err := os.ReadFile(marker); err == nil && trimSpace(string(b)) == p.Sum {
		return dir, nil
	}

	tmp := dir + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := p.Extract(tmp); err != nil {
		return "", fmt.Errorf("解压运行时失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".payload"), []byte(p.Sum), 0o644); err != nil {
		return "", err
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func runtimeCacheRoot(appID string) string {
	if isWindows() {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "gobrowser", appID)
		}
	}
	if v, err := os.UserCacheDir(); err == nil {
		return filepath.Join(v, "gobrowser", appID)
	}
	return filepath.Join(os.TempDir(), "gobrowser", appID)
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && !startsWithDotDot(rel)
}

func startsWithDotDot(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// String 便于日志输出。
func (p *Payload) String() string {
	return fmt.Sprintf("payload %d bytes @%#x sha=%s", p.Size, p.Offset, p.Sum)
}
