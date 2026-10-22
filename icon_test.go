package gobrowser

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeShell 起一个假外壳，把收到的请求记下来，返回体由 reply 决定。
type fakeShell struct {
	gotPath string
	gotBody map[string]any
	reply   string
}

func (f *fakeShell) server(t *testing.T) *App {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		f.gotBody = nil
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &f.gotBody)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("缺少 Authorization 头: %q", r.Header.Get("Authorization"))
		}
		if f.reply == "" {
			f.reply = `{"ok":true,"data":{}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, f.reply)
	}))
	t.Cleanup(ts.Close)
	return &App{base: ts.URL, token: "tok", client: ts.Client()}
}

// 相对路径必须转绝对（底座工作目录是 app.json 所在目录，相对路径必然找不到）。
func TestWindowSetIconUsesAbsPath(t *testing.T) {
	f := &fakeShell{}
	app := f.server(t)

	rel := filepath.Join("testdata", "busy.png") // 不要求文件存在：接口只负责传路径
	if err := app.Window("main").SetIcon(rel); err != nil {
		t.Fatalf("SetIcon: %v", err)
	}
	if f.gotPath != "/v1/windows/main/icon" {
		t.Fatalf("路径 = %q", f.gotPath)
	}
	got, _ := f.gotBody["icon"].(string)
	if !filepath.IsAbs(got) {
		t.Fatalf("icon 不是绝对路径: %q", got)
	}
	if filepath.Base(got) != "busy.png" {
		t.Fatalf("icon = %q", got)
	}
}

// 空串 = 恢复默认，底座据此回退到 app.json 的 window.icon。
func TestWindowSetIconEmptyRestoresDefault(t *testing.T) {
	f := &fakeShell{}
	app := f.server(t)
	if err := app.Window("main").SetIcon(""); err != nil {
		t.Fatalf("SetIcon(\"\"): %v", err)
	}
	if got := f.gotBody["icon"]; got != "" {
		t.Fatalf("icon = %v，期望空串", got)
	}
}

// 应用级换图标：走 /v1/icon（所有窗口 + 托盘 + Dock）。
func TestSetAppIcon(t *testing.T) {
	f := &fakeShell{}
	app := f.server(t)
	if err := app.SetAppIcon("icon.png"); err != nil {
		t.Fatalf("SetAppIcon: %v", err)
	}
	if f.gotPath != "/v1/icon" {
		t.Fatalf("路径 = %q", f.gotPath)
	}
	if got, _ := f.gotBody["icon"].(string); !filepath.IsAbs(got) || filepath.Base(got) != "icon.png" {
		t.Fatalf("icon = %q", got)
	}

	// 没启动时 Shell 侧要明确报错，而不是静默成功
	if err := New(Config{Title: "x"}).SetIcon("icon.png"); err == nil {
		t.Fatal("未启动时 SetIcon 应当报错")
	}
}

// 底座错误体里的 code 是数字；以前按 string 解析会整条退化成"响应不是 JSON"，
// 真正原因（图标文件不存在）被吞掉。
func TestDoSurfacesShellErrorMessage(t *testing.T) {
	f := &fakeShell{reply: `{"ok":false,"error":{"code":400,"message":"图标文件不存在: D:\\x\\nope.png"}}`}
	app := f.server(t)

	_, err := app.do(http.MethodPost, "/v1/icon", map[string]any{"icon": "x"})
	if err == nil {
		t.Fatal("期望报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "图标文件不存在") {
		t.Fatalf("错误信息丢了真正原因: %q", msg)
	}
	if !strings.HasPrefix(msg, "400:") {
		t.Fatalf("错误信息应带状态码: %q", msg)
	}
}

// TrayOptions.Icon 也要 Abs，否则托盘图标静默不显示。
func TestSetTrayAbsIcon(t *testing.T) {
	f := &fakeShell{}
	app := f.server(t)
	if err := app.SetTray(TrayOptions{Icon: filepath.Join("web", "icon.png"), Tooltip: "t"}); err != nil {
		t.Fatalf("SetTray: %v", err)
	}
	if f.gotPath != "/v1/tray" {
		t.Fatalf("路径 = %q", f.gotPath)
	}
	got, _ := f.gotBody["icon"].(string)
	if !filepath.IsAbs(got) {
		t.Fatalf("托盘 icon 不是绝对路径: %q", got)
	}
}

// absPath 的边界：空串保持空（恢复默认），绝对路径原样。
func TestAbsPath(t *testing.T) {
	if got := absPath(""); got != "" {
		t.Fatalf("空串 = %q", got)
	}
	abs := filepath.Join(os.TempDir(), "a.png")
	if got := absPath(abs); got != abs {
		t.Fatalf("绝对路径被改动: %q", got)
	}
}
