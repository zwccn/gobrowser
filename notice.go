package gobrowser

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func isWindows() bool { return runtime.GOOS == "windows" }

// LogPath 返回某个 appId 的日志文件路径（自动按平台选目录）。
func LogPath(appID string) string {
	if appID == "" {
		appID = "gobrowser"
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.TempDir()
		}
		return filepath.Join(base, appID, "logs", "shell.log")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Logs", appID, "shell.log")
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, appID, "logs", "shell.log")
	}
}

// AppendLog 往日志文件追加一行（没有控制台时这是唯一的现场）。
func AppendLog(appID, msg string) error {
	p := LogPath(appID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)
	return err
}

// Fatal 用于"程序是 GUI 子系统、没有控制台"的场景：
// 同时写日志 + 弹原生错误框 + 打 stderr（有控制台时才可见）。
// 调用方随后应 os.Exit(2)。
func Fatal(appID string, err error) {
	if err == nil {
		return
	}
	p := LogPath(appID)
	_ = AppendLog(appID, "FATAL "+err.Error())
	fmt.Fprintf(os.Stderr, "[gobrowser] %v\n", err)
	// GOBROWSER_SILENT=1 跳过弹框（CI / 自动化）；WEBSHELL_SILENT 是改名前的名字，继续兼容。
	silent := os.Getenv("GOBROWSER_SILENT") != "" || os.Getenv("WEBSHELL_SILENT") != ""
	if !silent {
		showErrorBox("gobrowser 启动失败", fmt.Sprintf("%v\n\n日志: %s", err, p))
	}
}
