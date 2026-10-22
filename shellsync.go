package gobrowser

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/zwccn/gobrowser/internal/asar"
)

// 底座（外壳 JS）随 SDK 走，而不是只烤在 runtime 里。
//
// 背景：`app/main.js` 会被打进 `runtime/resources/app.asar`。以前 SDK 加了新的 HTTP 动作
// （比如 659c20d 的 `POST /v1/icon`），老 runtime 里的 asar 没有这段代码，就会返回
// `unknown action: icon` —— 用户只升级 SDK 是拿不到新功能的，必须让人重发几十 MB 的运行时包。
//
// 现在：SDK 把 `app/` 用 go:embed 编进自己（约 24 KB），启动外壳前比对
// `<runtime>/resources/shell.version`，runtime 里的底座更旧就原子替换掉
// （原件备份成 app.asar.bak）。于是"改底座 JS"只需发 SDK，不必重发运行时。
//
// 兼容与安全：
//   - runtime 自带的底座更新（shell.version 更高）时**不动**，避免 SDK 版本落后把运行时降级；
//   - 老 runtime 没有 shell.version，按 0.0.0 处理，即"会被更新"；
//   - 目标文件与内置内容完全一致时只补写标记，不碰文件；
//   - runtime 目录只读 / 文件被占用时只返回错误，不中断启动（外壳继续用旧底座）；
//   - `GOBROWSER_SHELL_SYNC=0` 可整体关掉这个行为（例如运行时是签名分发、要求只读）。
//
//go:embed app
var shellFS embed.FS

// shellMarker 记录"这份 app.asar 是哪版底座"，放在 resources/ 下与 asar 同级。
const shellMarker = "shell.version"

// ShellVersion 返回当前 SDK 内置底座的版本（来自 app/package.json 的 shellVersion）。
func ShellVersion() string {
	_, v, _ := shellBundle()
	return v
}

// shellVer 返回"这次实际在用的底座版本"，仅用于错误提示：
//   - 标记比 SDK 新 → 返回标记（运行时自带的更新，SDK 没覆盖）
//   - 没有标记 → 老运行时（同步前的发行版）
//   - 其余 → SDK 版本（说明同步已经生效）
func (a *App) shellVer() string {
	if a.rt.Dir == "" {
		return "unknown"
	}
	v := readFileString(filepath.Join(a.rt.Dir, "resources", shellMarker))
	switch {
	case v == "":
		return "老版本（无标记）"
	case cmpShellVersion(v, ShellVersion()) > 0:
		return v
	}
	return ShellVersion()
}

var (
	shellOnce sync.Once
	shellBin  []byte
	shellVer  string
	shellErr  error
)

// shellBundle 惰性地把内置 app/ 打成 asar（只做一次，结果在内存里复用）。
func shellBundle() ([]byte, string, error) {
	shellOnce.Do(func() {
		sub, err := fs.Sub(shellFS, "app")
		if err != nil {
			shellErr = err
			return
		}
		if b, err := fs.ReadFile(sub, "package.json"); err == nil {
			var pkg struct {
				ShellVersion string `json:"shellVersion"`
			}
			if json.Unmarshal(b, &pkg) == nil {
				shellVer = pkg.ShellVersion
			}
		}
		if shellVer == "" {
			shellVer = "0.0.0"
		}
		shellBin, shellErr = packShellFS(sub)
	})
	return shellBin, shellVer, shellErr
}

// syncShell 让 runtime 里的 app.asar 与 SDK 内置底座对齐；已经够新就什么都不做。
func syncShell(rtDir string) error {
	if shellSyncDisabled() {
		return nil
	}
	data, ver, err := shellBundle()
	if err != nil {
		return fmt.Errorf("打包内置底座失败: %w", err)
	}
	resDir := filepath.Join(rtDir, "resources")
	target := filepath.Join(resDir, "app.asar")
	marker := filepath.Join(resDir, shellMarker)

	cur := strings.TrimSpace(readFileString(marker))
	if cmpShellVersion(cur, ver) >= 0 {
		return nil // 运行时自带的不比 SDK 旧
	}

	if sameFile(target, data) {
		// 内容一样，只补标记（第一次给老 runtime 打标就是走这里）
		return writeFileAtomic(marker, []byte(ver+"\n"))
	}

	// 备份一次原件，便于回滚
	bak := target + ".bak"
	if _, err := os.Stat(bak); os.IsNotExist(err) {
		if _, err := os.Stat(target); err == nil {
			if err := copyFile(target, bak); err != nil {
				return fmt.Errorf("备份 %s 失败: %w", target, err)
			}
		}
	}

	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败（运行时目录只读？）: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp) // Windows 下目标被占用时 rename 会失败，下次启动再试
		return fmt.Errorf("替换 %s 失败（外壳正在运行？）: %w", target, err)
	}
	return writeFileAtomic(marker, []byte(ver+"\n"))
}

// ShellSyncDisabled 报告底座自动同步是否被 GOBROWSER_SHELL_SYNC=0 关掉。
func shellSyncDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOBROWSER_SHELL_SYNC"))) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// packShellFS 只是给 asar.PackFS 起个短名字，免得本文件里到处 import 前缀。
func packShellFS(fsys fs.FS) ([]byte, error) { return asar.PackFS(fsys) }

// cmpShellVersion 比较 "1.2.3" 形式的版本；缺失段按 0，非数字后缀忽略。返回 -1/0/1。
func cmpShellVersion(a, b string) int {
	as, bs := splitVersion(a), splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		// 只取前导数字：1.2.0-r2 / 1.2.0+build 都按 1.2.0 处理
		i := 0
		for i < len(p) && p[i] >= '0' && p[i] <= '9' {
			i++
		}
		n, _ := strconv.Atoi(p[:i])
		out = append(out, n)
	}
	return out
}

func readFileString(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(b))
}

func sameFile(p string, want []byte) bool {
	st, err := os.Stat(p)
	if err != nil || st.Size() != int64(len(want)) {
		return false
	}
	got, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return bytes.Equal(got, want)
}

func writeFileAtomic(p string, data []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
