// doctor：一条命令体检运行时与配置。
//
//	go run ./tools/doctor -app C:\Apps\app.json
//	go run ./tools/doctor -dir C:\Apps
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zwccn/gobrowser"
	"github.com/zwccn/gobrowser/internal/payloadfmt"
)

type check struct {
	icon string
	name string
	msg  string
}

func main() {
	appJSON := flag.String("app", "", "app.json 路径")
	dir := flag.String("dir", "", "程序所在目录（与 -app 二选一）")
	flag.Parse()

	var base string
	if *appJSON != "" {
		abs, err := filepath.Abs(*appJSON)
		if err != nil {
			die("app.json 路径无效: %v", err)
		}
		base = filepath.Dir(abs)
	} else if *dir != "" {
		abs, _ := filepath.Abs(*dir)
		base = abs
	} else {
		exe, _ := os.Executable()
		base = filepath.Dir(exe)
	}

	var cs []check
	add := func(icon, name, format string, args ...any) {
		cs = append(cs, check{icon, name, fmt.Sprintf(format, args...)})
	}

	// 1) app.json
	cfgPath := filepath.Join(base, "app.json")
	cfg := struct {
		AppID         string `json:"appId"`
		Name          string `json:"name"`
		Version       string `json:"version"`
		PreferBundled bool   `json:"preferBundled"`
		GPU           string `json:"gpu"`
		CloseToTray   bool   `json:"closeToTray"`
	}{}
	appID := "webshell"
	if b, err := os.ReadFile(cfgPath); err != nil {
		if os.IsNotExist(err) {
			add("i", "app.json", "没有（类库模式下由 Config 自动生成，正常）")
		} else {
			add("✗", "app.json", "读不到: %v", err)
		}
	} else if err := json.Unmarshal(b, &cfg); err != nil {
		add("✗", "app.json", "解析失败: %v", err)
	} else {
		if cfg.AppID == "" {
			add("✗", "app.json", "缺少 appId（隔离锚点）")
		} else {
			appID = cfg.AppID
			add("✓", "app.json", "%s v%s · appId=%s", cfg.Name, cfg.Version, cfg.AppID)
		}
	}

	// 2) 单文件载荷（exe 尾部）
	hasPayload := false
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".exe") {
				continue
			}
			if tr, ok := payloadfmt.Read(filepath.Join(base, e.Name())); ok {
				hasPayload = true
				add("✓", "单文件载荷", "%s 内含 %.1f MB runtime（首次运行释放到用户缓存）",
					e.Name(), float64(tr.Size)/1024/1024)
			}
		}
	}

	// 3) 运行时
	rt, err := gobrowser.FindRuntimeAt(base, cfg.PreferBundled)
	if err != nil && hasPayload {
		add("i", "运行时", "同级没有 runtime，将由 exe 内载荷自动释放")
	} else if err != nil {
		add("✗", "运行时", "%v", err)
	} else {
		add("✓", "运行时", "%s · version=%s", rt.Dir, orDash(rt.Version))
		add("✓", "外壳程序", "%s", rt.Exe)
		for _, rel := range []string{"electron.sign", filepath.Join("resources", "app.asar")} {
			p := filepath.Join(rt.Dir, rel)
			if fi, err := os.Stat(p); err == nil {
				add("✓", rel, "%d B", fi.Size())
			} else {
				add("✗", rel, "缺失")
			}
		}
		if entries, err := os.ReadDir(filepath.Join(rt.Dir, "lic")); err == nil {
			n := 0
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".lic") {
					n++
				}
			}
			if n == 0 {
				add("!", "lic/", "目录存在但没有 .lic 授权文件")
			} else {
				add("✓", "lic/", "%d 个授权文件", n)
			}
		} else {
			add("✗", "lic/", "目录缺失")
		}
		if _, err := os.Stat(filepath.Join(rt.Dir, "resources", "app.asar")); err == nil {
			add("✓", "底座代码", "resources/app.asar 就位")
		}
	}

	// 4) 用户数据 / 日志目录可写
	userData := filepath.Join(os.Getenv("APPDATA"), appID)
	if userData == filepath.Join("", appID) { // 非 Windows
		userData = filepath.Join(os.TempDir(), appID)
	}
	if err := ensureWritable(userData); err != nil {
		add("✗", "数据目录", "%s 不可写: %v", userData, err)
	} else {
		add("✓", "数据目录", "%s（按 appId 隔离）", userData)
	}
	logPath := gobrowser.LogPath(appID)
	if err := ensureWritable(filepath.Dir(logPath)); err != nil {
		add("✗", "日志目录", "%v", err)
	} else {
		add("✓", "日志目录", "%s", logPath)
	}

	// 5) 端口
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err != nil {
		add("✗", "端口", "无法绑定 127.0.0.1: %v", err)
	} else {
		p := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		add("✓", "端口", "可绑定 127.0.0.1:%d（每次启动随机分配）", p)
	}

	// 6) 环境提示
	if v := os.Getenv("SESSIONNAME"); strings.HasPrefix(strings.ToUpper(v), "RDP") {
		if cfg.GPU != "disable" {
			add("!", "显卡", "检测到远程桌面会话，建议 app.json 设 \"gpu\": \"disable\"")
		}
	}
	if cfg.CloseToTray {
		add("i", "关闭行为", "closeToTray=true：点关闭会收进托盘")
	}

	// 输出
	fmt.Println("WebShell doctor")
	fmt.Println("目录:", base)
	fmt.Println(strings.Repeat("─", 68))
	w := 0
	for _, c := range cs {
		if len(c.name) > w {
			w = len(c.name)
		}
	}
	bad := 0
	for _, c := range cs {
		fmt.Printf(" %s %-*s  %s\n", c.icon, w+2, c.name, c.msg)
		if c.icon == "✗" {
			bad++
		}
	}
	fmt.Println(strings.Repeat("─", 68))
	if bad > 0 {
		fmt.Printf("发现 %d 个问题\n", bad)
		os.Exit(1)
	}
	fmt.Println("全部通过")
}

func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
		return err
	}
	_ = os.Remove(f)
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func die(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
	time.Sleep(10 * time.Millisecond)
	os.Exit(2)
}
