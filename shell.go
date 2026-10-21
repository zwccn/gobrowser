// Package gobrowser 是 gobrowser 的 Go 侧 SDK：
// 找到同级 runtime、拉起外壳进程、读握手、提供窗口控制 API。
package gobrowser

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Options 启动参数。AppJSON 与 Handler 是必填的两项。
type Options struct {
	AppJSON    string       // app.json 路径（相对或绝对）
	Handler    http.Handler // 业务 UI（同时作为事件回调端点）
	EntryURL   string       // 可选：直接指定页面地址（给了就不再用内置 UI 服务）
	RuntimeDir string       // 可选：显式指定 runtime 目录（不给则按同级目录查找）
	OnEvent    func(Event)  // 可选：外壳事件回调
	Env        []string     // 追加的环境变量
	Debug      bool         // 把外壳 stdout/stderr 透传到本进程

	handshakeTimeout time.Duration
}

// Event 是外壳上报的事件。
type Event struct {
	Name     string          `json:"event"`
	AppID    string          `json:"appId"`
	WindowID string          `json:"windowId"`
	Ts       int64           `json:"ts"`
	Payload  json.RawMessage `json:"payload"`
}

// App 代表一个正在运行的外壳实例。
type App struct {
	cfg     appConfig
	rt      RuntimeInfo
	cmd     *exec.Cmd
	base    string
	token   string
	uiPort  int
	ui      *http.Server
	client  *http.Client
	events  chan Event
	done    chan struct{}
	waitErr error
	once    sync.Once
	Debug   bool
}

type appConfig struct {
	AppID         string   `json:"appId"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	PreferBundled bool     `json:"preferBundled"`
	GPU           string   `json:"gpu"`
	ExtraSwitches []string `json:"extraSwitches"`
	Entry         struct {
		URL     string `json:"url"`
		WaitFor struct {
			TimeoutMs  int `json:"timeoutMs"`
			IntervalMs int `json:"intervalMs"`
		} `json:"waitFor"`
	} `json:"entry"`
	Window map[string]any `json:"window"`
}

type handshake struct {
	Event      string `json:"event"`
	Port       int    `json:"port"`
	Token      string `json:"token"`
	PID        int    `json:"pid"`
	Electron   string `json:"electron"`
	Browser360 string `json:"browser360"`
}

// Start 找运行时 → 拉起外壳 → 等握手。返回后窗口已经在创建中。
func Start(o Options) (*App, error) {
	if o.AppJSON == "" {
		return nil, errors.New("shell: Options.AppJSON 必填")
	}
	appJSON, err := filepath.Abs(o.AppJSON)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(appJSON)
	if err != nil {
		return nil, fmt.Errorf("读取 app.json 失败: %w", err)
	}
	var cfg appConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析 app.json 失败: %w", err)
	}
	if cfg.AppID == "" {
		return nil, errors.New("app.json 缺少 appId（隔离锚点，必须唯一）")
	}

	var rt RuntimeInfo
	if o.RuntimeDir != "" {
		r, missing := Probe(o.RuntimeDir)
		if len(missing) > 0 {
			return nil, fmt.Errorf("指定的 runtime 目录不完整: %s 缺少 %v", o.RuntimeDir, missing)
		}
		r.Version = readVersion(o.RuntimeDir)
		rt = r
	} else {
		var err error
		rt, err = FindRuntime(cfg.PreferBundled)
		if err != nil {
			return nil, err
		}
	}

	// 1) 业务 UI 服务 + 事件端点
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	uiPort := ln.Addr().(*net.TCPAddr).Port
	a := &App{
		cfg:    cfg,
		rt:     rt,
		uiPort: uiPort,
		client: &http.Client{Timeout: 15 * time.Second},
		events: make(chan Event, 64),
		done:   make(chan struct{}),
		Debug:  o.Debug,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/_shell/events", a.onEventHTTP)
	if o.Handler != nil {
		mux.Handle("/", o.Handler)
	}
	a.ui = &http.Server{Handler: mux}
	go a.ui.Serve(ln)

	entry := o.EntryURL
	if entry == "" {
		entry = fmt.Sprintf("http://127.0.0.1:%d/", uiPort)
	}
	events := fmt.Sprintf("http://127.0.0.1:%d/_shell/events", uiPort)

	// 2) 拉起外壳
	args := []string{"--app=" + appJSON, "--entry=" + entry, "--events=" + events}
	// GPU 开关走命令行：与 app.commandLine.appendSwitch 等效，但不用改 asar，
	// 旧 runtime 配合新 SDK 也立刻生效。
	args = append(args, gpuSwitches(cfg)...)
	args = append(args, cfg.ExtraSwitches...)
	cmd := exec.Command(rt.Exe, args...)
	cmd.Dir = filepath.Dir(appJSON)
	cmd.Env = cleanEnv(o.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.shutdownUI()
		return nil, err
	}
	var errBuf bytes.Buffer
	if o.Debug {
		cmd.Stderr = io.MultiWriter(os.Stderr, &errBuf)
	} else {
		cmd.Stderr = &errBuf
	}

	if err := cmd.Start(); err != nil {
		a.shutdownUI()
		return nil, fmt.Errorf("启动外壳失败（%s）: %w", rt.Exe, err)
	}
	a.cmd = cmd

	// 3) 读握手：跳过空行，取第一行 JSON
	timeout := o.handshakeTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	hs, err := readHandshake(stdout, timeout)
	if err != nil {
		_ = cmd.Process.Kill()
		a.shutdownUI()
		return nil, fmt.Errorf("握手失败: %w（外壳 stderr: %s）", err, firstLine(errBuf.String()))
	}
	a.base = fmt.Sprintf("http://127.0.0.1:%d", hs.Port)
	a.token = hs.Token
	a.rt.Electron = hs.Electron
	a.rt.Browser360 = hs.Browser360

	// 4) 后台等进程退出 + 事件分发
	go func() {
		err := cmd.Wait()
		a.waitErr = err
		close(a.done)
		a.shutdownUI()
		close(a.events)
	}()
	if o.OnEvent != nil {
		go func() {
			for ev := range a.events {
				o.OnEvent(ev)
			}
		}()
	}
	return a, nil
}

func readHandshake(r io.Reader, timeout time.Duration) (*handshake, error) {
	type result struct {
		hs  *handshake
		err error
	}
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		for i := 0; i < 20; i++ {
			line, err := br.ReadString('\n')
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "{") {
				var hs handshake
				if json.Unmarshal([]byte(line), &hs) == nil && hs.Event == "ready" && hs.Port > 0 {
					ch <- result{&hs, nil}
					return
				}
			}
			if err != nil {
				ch <- result{nil, fmt.Errorf("外壳未输出握手行: %w", err)}
				return
			}
		}
		ch <- result{nil, errors.New("没有读到握手 JSON")}
	}()
	select {
	case r := <-ch:
		return r.hs, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("等待握手上限 %s", timeout)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return strings.TrimSpace(s)
}

func cleanEnv(extra []string) []string {
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "ELECTRON_RUN_AS_NODE=") || strings.HasPrefix(e, "NODE_OPTIONS=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}

func (a *App) onEventHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var ev Event
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := json.Unmarshal(body, &ev); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var p struct {
		WindowID string `json:"windowId"`
	}
	_ = json.Unmarshal(ev.Payload, &p)
	ev.WindowID = p.WindowID
	select {
	case a.events <- ev:
	default: // 缓冲满就丢，避免阻塞外壳
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) shutdownUI() {
	a.once.Do(func() {
		if a.ui != nil {
			_ = a.ui.Close()
		}
	})
}

// ------------------------------------------------------------------ 公共 API

// Runtime 返回本次使用的运行时信息。
func (a *App) Runtime() RuntimeInfo { return a.rt }

// UIPort 返回业务 UI 实际监听的端口。
func (a *App) UIPort() int { return a.uiPort }

// Events 返回事件流（外壳退出时关闭）。如果设置了 OnEvent，不要再消费它。
func (a *App) Events() <-chan Event { return a.events }

// Wait 阻塞到外壳进程退出（窗口全关或调 Quit）。
func (a *App) Wait() error {
	<-a.done
	return a.waitErr
}

// Quit 优雅关闭外壳。
func (a *App) Quit() error {
	_, err := a.do(http.MethodPost, "/v1/shutdown", nil)
	return err
}

// Close 关闭外壳并释放资源。
func (a *App) Close() {
	_ = a.Quit()
	select {
	case <-a.done:
	case <-time.After(3 * time.Second):
		if a.cmd != nil && a.cmd.Process != nil {
			_ = a.cmd.Process.Kill()
		}
	}
	a.shutdownUI()
}

// Info 返回外壳自报信息（版本、窗口列表、bridge 调用计数等）。
func (a *App) Info() (map[string]any, error) {
	return a.do(http.MethodGet, "/v1/info", nil)
}

// Client 暴露底层访问（SDK 没包的能力可以直接打 HTTP）。
func (a *App) Client() *Client { return &Client{a: a} }

type Client struct{ a *App }

func (c *Client) Do(method, path string, body any) (map[string]any, error) {
	return c.a.do(method, path, body)
}

func (a *App) do(method, path string, body any) (map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		OK    bool           `json:"ok"`
		Data  map[string]any `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("响应不是 JSON（%d）: %s", resp.StatusCode, string(raw))
	}
	if !out.OK {
		return nil, fmt.Errorf("%s: %s", out.Error.Code, out.Error.Message)
	}
	return out.Data, nil
}

// Capturer 供 Window.Capture 使用
func decodePNG(b64 string) ([]byte, error) { return base64.StdEncoding.DecodeString(b64) }

// gpuSwitches 按 app.json 的 gpu 模式返回 Chromium 开关。
//
// "safe" 是 VM / 远程桌面 / 无核显机器的防闪退组合：只关硬件加速不够
// （实测窗口会闪退并 FATAL），这五个开关是逐个试出来的有效组合。
func gpuSwitches(cfg appConfig) []string {
	switch cfg.GPU {
	case "safe":
		return []string{
			"--disable-gpu",
			"--disable-gpu-compositing",
			"--disable-software-rasterizer",
			"--disable-dev-shm-usage",
			"--no-sandbox",
		}
	case "disable":
		return []string{"--disable-gpu"}
	}
	return nil
}
