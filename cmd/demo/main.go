// 示例：一个最小的 Go 业务程序，用 WebShell 显示窗口。
//
//	go run ./cmd/demo -hold=3s          # 开发态（有控制台，能看到日志）
//	demo.exe                            # 发布态（GUI 子系统，无控制台，日志写文件）
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zwccn/gobrowser"
)

//go:embed web
var webFS embed.FS

func main() {
	appJSON := flag.String("app", "", "app.json 路径（默认取 exe 同目录的 app.json）")
	hold := flag.Duration("hold", 0, "运行多久后自动退出（0 = 一直开着）")
	logPath := flag.String("log", "", "日志文件路径（默认按 appId 写用户目录）")
	flag.Parse()

	cfgPath := *appJSON
	if cfgPath == "" {
		exe, _ := os.Executable()
		cfgPath = filepath.Join(filepath.Dir(exe), "app.json")
	}
	appID := peekAppID(cfgPath)

	// 日志：没有控制台时这是唯一的现场（%LOCALAPPDATA%\<appId>\logs\gobrowser.log）
	path := *logPath
	if path == "" {
		path = gobrowser.LogPath(appID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			defer f.Close()
			log.SetOutput(f)
		}
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("[boot] exe=%s app=%s", os.Args[0], cfgPath)

	// 业务 UI：Go 自己托管页面（前端可以 embed 进来，exe 依然只有几 MB）
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		gobrowser.Fatal(appID, err)
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))

	// 启动外壳
	app, err := gobrowser.Start(gobrowser.Options{
		AppJSON: cfgPath,
		Handler: mux,
		OnEvent: func(e gobrowser.Event) {
			log.Printf("[event] %s window=%s payload=%s", e.Name, e.WindowID, string(e.Payload))
		},
	})
	if err != nil {
		// GUI 程序没有控制台：写日志 + 弹原生错误框，用户才看得见
		gobrowser.Fatal(appID, err)
		os.Exit(2)
	}
	defer app.Close()

	rt := app.Runtime()
	log.Printf("[runtime] dir=%s exe=%s version=%s electron=%s browser360=%s",
		rt.Dir, rt.Exe, rt.Version, rt.Electron, rt.Browser360)
	log.Printf("[ui] port=%d log=%s", app.UIPort(), path)

	w := app.Window("main")
	time.Sleep(1200 * time.Millisecond)

	if info, err := app.Info(); err == nil {
		log.Printf("[info-before] windows=%v tray=%v", info["windows"], info["tray"])
	}

	// 托盘
	if err := app.SetTray(gobrowser.TrayOptions{
		Tooltip: "报表中心",
		Menu: []gobrowser.MenuItem{
			{ID: "show", Label: "显示主窗口", Action: "show"},
			{ID: "about", Label: "关于"},
			{Type: "separator"},
			{ID: "quit", Label: "退出", Action: "quit"},
		},
	}); err != nil {
		log.Printf("[warn] SetTray: %v", err)
	} else {
		log.Printf("[tray] 已创建")
	}

	// 通知
	if err := app.Notify("报表中心", "已启动并连接到 127.0.0.1"); err != nil {
		log.Printf("[warn] Notify: %v", err)
	} else {
		log.Printf("[notify] 已发送")
	}

	// 窗口控制
	if err := w.SetTitle("报表中心 · 已连接"); err != nil {
		log.Printf("[warn] SetTitle: %v", err)
	}
	if err := w.Maximize(); err != nil {
		log.Printf("[warn] Maximize: %v", err)
	} else {
		time.Sleep(400 * time.Millisecond)
		if max, err := w.IsMaximized(); err == nil {
			log.Printf("[maximize] isMaximized=%v", max)
		}
	}
	time.Sleep(600 * time.Millisecond)
	if info, err := app.Info(); err == nil {
		log.Printf("[info-after] windows=%v tray=%v bridgeCalls=%v", info["windows"], info["tray"], info["bridgeCalls"])
	}

	if *hold > 0 {
		log.Printf("[hold] %s 后退出", *hold)
		time.Sleep(*hold)
		_ = w.Close()
	}

	log.Printf("[wait] 窗口关闭，等待外壳退出")
	log.Printf("[done] %v", app.Wait())
}

func peekAppID(cfgPath string) string {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return "webshell"
	}
	var h struct {
		AppID string `json:"appId"`
	}
	if json.Unmarshal(b, &h) != nil || h.AppID == "" {
		return "webshell"
	}
	return h.AppID
}
