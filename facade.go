package gobrowser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"io/fs"
)

// Config 是"类库用法"的入口配置。除 Title 外全部有默认值。
//
//	shell.New(shell.Config{Title: "报表中心"}).ShowDir("./web")
type Config struct {
	// 身份
	AppID string // 缺省 = local.<exe 文件名>；决定数据隔离目录，同一产品应固定
	Title string // 窗口标题 / 托盘名；缺省 = exe 文件名

	// 窗体
	Width, Height       int  // 缺省 1024×720
	MinWidth, MinHeight int  // 可选
	Frameless           bool // 无边框（页面自己画标题栏）
	Resizable           *bool
	AlwaysOnTop         bool
	Icon                string // 窗口/托盘图标路径（png/ico）

	// 行为
	GPU         string       // "" = 自动；"disable" = 关硬件加速（虚拟机/RDP 建议）
	CloseToTray bool         // 点关闭收进托盘而不是退出
	Tray        *TrayOptions // 非 nil 则自动创建托盘
	OnEvent     func(Event)  // 可选：收低频事件
	Verbose     bool         // 把外壳日志透传到 stderr
	ConfigFile  string       // 直接指定现成 app.json（一般不用）
	RuntimeDir  string       // 显式指定 runtime 目录；留空则自动（同级目录 → 内嵌载荷）
}

// Shell 是类库门面：New 之后调 Show/ShowDir/ShowFS/ShowURL 即可。
type Shell struct {
	cfg      Config
	mux      *http.ServeMux
	hasRoot  bool
	entryURL string
	app      *App
	mu       sync.Mutex
}

// New 构造一个 Shell（不会启动任何东西，纯内存操作）。
func New(cfg Config) *Shell {
	base := exeBaseName()
	if cfg.Title == "" {
		cfg.Title = base
	}
	if cfg.AppID == "" {
		cfg.AppID = "local." + sanitize(base)
	}
	if cfg.Width <= 0 {
		cfg.Width = 1024
	}
	if cfg.Height <= 0 {
		cfg.Height = 720
	}
	if cfg.Icon != "" {
		if abs, err := filepath.Abs(cfg.Icon); err == nil {
			cfg.Icon = abs
		}
	}
	return &Shell{cfg: cfg, mux: http.NewServeMux()}
}

// NewFromFile 用现成的 app.json 初始化（底层模式与类库模式可以混用）。
func NewFromFile(appJSON string) *Shell {
	s := New(Config{ConfigFile: appJSON})
	return s
}

// ---------------------------------------------------------------- 路由

// Handle 注册一个业务路由（页面用的 API 就挂在这里，和 UI 同一个服务）。
func (s *Shell) Handle(pattern string, h http.Handler) *Shell {
	s.mux.Handle(pattern, h)
	if pattern == "/" {
		s.hasRoot = true
	}
	return s
}

// HandleFunc 同上，函数形式。
func (s *Shell) HandleFunc(pattern string, f http.HandlerFunc) *Shell {
	return s.Handle(pattern, f)
}

// ---------------------------------------------------------------- 展示页面

// Show 显示内置欢迎页（不需要任何前端文件，用来做 hello world）。
func (s *Shell) Show() error {
	if !s.hasRoot {
		s.mux.HandleFunc("/", welcomeHandler(s.cfg.Title))
		s.hasRoot = true
	}
	return s.Run()
}

// ShowDir 把本地目录当作页面根目录。
func (s *Shell) ShowDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return fmt.Errorf("页面目录不存在: %s", abs)
	}
	if !s.hasRoot {
		s.mux.Handle("/", http.FileServer(http.Dir(abs)))
		s.hasRoot = true
	}
	return s.Run()
}

// ShowFS 用 embed.FS 之类的只读文件系统（把前端编译进 exe）。
//
//	//go:embed web
//	var webFS embed.FS
//	sub, _ := fs.Sub(webFS, "web")
//	shell.New(cfg).ShowFS(sub)
func (s *Shell) ShowFS(fsys fs.FS) error {
	if !s.hasRoot {
		s.mux.Handle("/", http.FileServer(http.FS(fsys)))
		s.hasRoot = true
	}
	return s.Run()
}

// ShowURL 直接指向已有的后端地址（页面由别的服务提供）。
func (s *Shell) ShowURL(url string) error {
	s.entryURL = url
	return s.Run()
}

// ShowHandler 用自定义 Handler 提供页面（适合自带路由的框架）。
func (s *Shell) ShowHandler(h http.Handler) error {
	if !s.hasRoot {
		s.mux.Handle("/", h)
		s.hasRoot = true
	}
	return s.Run()
}

// ---------------------------------------------------------------- 生命周期

// Run = Start + Wait（阻塞，直到窗口关闭）。
func (s *Shell) Run() error {
	if err := s.Start(); err != nil {
		return err
	}
	return s.Wait()
}

// Start 启动但不阻塞；随后可用 s.Window() 控窗口、s.Wait() 等退出。
func (s *Shell) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.app != nil {
		return fmt.Errorf("已经启动过了")
	}

	// 运行时来源：显式指定 > 同级目录 > exe 内嵌载荷（首次解压到用户缓存）
	if s.cfg.RuntimeDir == "" {
		d, err := PrepareRuntime(s.cfg.AppID)
		switch {
		case err == nil:
			s.cfg.RuntimeDir = d // 可能是 ""（同级已有）
		case errors.Is(err, ErrNoPayload):
			// 既没同级 runtime 也没载荷，交给 Start 报标准错误
		default:
			return fmt.Errorf("准备运行时失败: %w", err)
		}
	}

	cfgPath := s.cfg.ConfigFile
	if cfgPath == "" {
		p, err := s.writeTempAppJSON()
		if err != nil {
			return err
		}
		cfgPath = p
	}

	app, err := Start(Options{
		AppJSON:    cfgPath,
		Handler:    s.mux,
		EntryURL:   s.entryURL,
		RuntimeDir: s.cfg.RuntimeDir,
		OnEvent:    s.cfg.OnEvent,
		Debug:      s.cfg.Verbose,
	})
	if err != nil {
		return err
	}
	s.app = app
	return nil
}

// Wait 阻塞到外壳退出。
func (s *Shell) Wait() error {
	if s.app == nil {
		return fmt.Errorf("还没启动")
	}
	return s.app.Wait()
}

// Quit 关闭窗口并退出外壳。
func (s *Shell) Quit() error {
	if s.app == nil {
		return nil
	}
	return s.app.Quit()
}

// Window 主窗口句柄（'main'）。
func (s *Shell) Window() *Window {
	if s.app == nil {
		return nil
	}
	return s.app.Window("main")
}

// App 底层对象（需要托盘/对话框/通知/原始 HTTP 时用）。
func (s *Shell) App() *App { return s.app }

// Close 释放资源。
func (s *Shell) Close() {
	if s.app != nil {
		s.app.Close()
	}
}

// writeTempAppJSON 把 Config 落成一份临时 app.json —— 开发者不必手写配置文件。
func (s *Shell) writeTempAppJSON() (string, error) {
	win := map[string]any{
		"width":  s.cfg.Width,
		"height": s.cfg.Height,
		"center": true,
	}
	if s.cfg.MinWidth > 0 {
		win["minWidth"] = s.cfg.MinWidth
	}
	if s.cfg.MinHeight > 0 {
		win["minHeight"] = s.cfg.MinHeight
	}
	if s.cfg.Frameless {
		win["frame"] = false
	}
	if s.cfg.Resizable != nil {
		win["resizable"] = *s.cfg.Resizable
	}
	if s.cfg.AlwaysOnTop {
		win["alwaysOnTop"] = true
	}
	if s.cfg.Icon != "" {
		win["icon"] = s.cfg.Icon
	}

	cfg := map[string]any{
		"appId":         s.cfg.AppID,
		"name":          s.cfg.Title,
		"version":       "1.0.0",
		"preferBundled": true,
		"window":        win,
	}
	if s.cfg.GPU != "" {
		cfg["gpu"] = s.cfg.GPU
	}
	if s.cfg.CloseToTray {
		cfg["closeToTray"] = true
	}
	if s.cfg.Tray != nil {
		t := s.cfg.Tray
		enabled := true
		cfg["tray"] = map[string]any{
			"enabled": enabled,
			"icon":    firstNonEmpty(t.Icon, s.cfg.Icon),
			"tooltip": firstNonEmpty(t.Tooltip, s.cfg.Title),
			"menu":    t.Menu,
		}
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(os.TempDir(), "gobrowser-"+sanitize(s.cfg.AppID)+".app.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// ---------------------------------------------------------------- 小工具

func exeBaseName() string {
	return ExeBaseName(mustExecutable())
}

// ExeBaseName 返回去掉扩展名的可执行文件名（Windows 上 demo.exe → demo）。
func ExeBaseName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func mustExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "app"
	}
	return exe
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// welcomeHandler 内置欢迎页：证明"引用类库 + Show"就能出窗口。
func welcomeHandler(title string) http.HandlerFunc {
	page := `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>` + title + `</title>
<style>
:root{color-scheme:dark}
body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
font-family:"Segoe UI","Microsoft YaHei",system-ui;background:radial-gradient(circle at 25% 10%,#17324d,#0b1220 65%);color:#e6eefb}
.card{padding:32px 40px;border:1px solid rgba(255,255,255,.14);border-radius:16px;background:rgba(255,255,255,.05);text-align:center}
h1{margin:0 0 8px;font-size:20px;font-weight:600}
p{margin:6px 0;font-size:13px;color:#8fa4bd}
code{color:#7fd1ff;font-family:Consolas,monospace}
.ok{color:#68d391}
</style>
<div class="card">
  <h1>` + title + `</h1>
  <p>这是 gobrowser 的内置欢迎页 —— 说明窗口已经由底座托管</p>
  <p class="ok" id="s">检测桥接中…</p>
  <p>下一步：<code>ShowDir("./web")</code> 或 <code>ShowFS(embedFS)</code> 换成你自己的页面</p>
</div>
<script>
if (window.webshell) {
  window.webshell.app.ping().then(function(r){
    document.getElementById('s').textContent = 'window.webshell 已注入 ✓  appId=' + r.appId
  })
} else {
  document.getElementById('s').textContent = 'window.webshell 未注入 ✗'
  document.getElementById('s').className = ''
}
</script></html>`
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
}
