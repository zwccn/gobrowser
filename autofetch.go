package gobrowser

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/zwccn/gobrowser/internal/runtimefetch"
)

// 自动获取运行时：只在"开发态"或显式开启时联网，生产构建永远不下载。
//
// 开关优先级：
//  1. GOBROWSER_AUTO_FETCH=0   强制关闭
//  2. GOBROWSER_AUTO_FETCH=1   强制开启（CI / 特殊部署可用）
//  3. 默认：只在 go run 时开启（见 isGoRun）
//
// 镜像与本地包：
//   - GOBROWSER_RUNTIME_BASE=https://mirror/…   换下载前缀
//   - GOBROWSER_RUNTIME_DIR=D:\shared\runtime   直接用本地 zip，完全不联网
//   - GOBROWSER_RUNTIME_CACHE=D:\cache          换缓存目录
func autoFetchEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOBROWSER_AUTO_FETCH"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	}
	return isGoRun()
}

// isGoRun 判断当前进程是不是 `go run` 编译出来的。
//
// go run 会把临时源码目录当包路径，于是 Main.Path 就是 "command-line-arguments"；
// go build 出来的可执行文件则是真实模块路径，所以生产包不会命中。
func isGoRun() bool {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	return bi.Main.Path == "command-line-arguments"
}

// fetchRuntime 下载（或从本地目录取）对应平台的运行时，返回可用目录。
func fetchRuntime() (string, error) {
	lastPct := int64(-1)
	progress := func(done, total int64) {
		if total <= 0 {
			fmt.Fprintf(os.Stderr, "\r[gobrowser] 获取运行时 %.1f MB", float64(done)/(1<<20))
			return
		}
		pct := done * 100 / total
		if pct == lastPct {
			return
		}
		lastPct = pct
		fmt.Fprintf(os.Stderr, "\r[gobrowser] 获取运行时 %3d%%  %.1f/%.1f MB",
			pct, float64(done)/(1<<20), float64(total)/(1<<20))
	}

	dir, err := runtimefetch.Ensure(runtime.GOOS, runtime.GOARCH, runtimefetch.Options{
		BaseURL:  os.Getenv("GOBROWSER_RUNTIME_BASE"),
		LocalDir: os.Getenv("GOBROWSER_RUNTIME_DIR"),
		Progress: progress,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "[gobrowser] "+format+"\n", args...)
		},
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("自动获取运行时失败: %w\n"+
			"  可改用：手动把 runtime\\ 放到程序同级，或设 GOBROWSER_AUTO_FETCH=0 关闭自动获取\n"+
			"  镜像：GOBROWSER_RUNTIME_BASE=https://你的镜像/  ·  本地包：GOBROWSER_RUNTIME_DIR=D:\\shared", err)
	}
	return dir, nil
}
