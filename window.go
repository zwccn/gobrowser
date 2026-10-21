package gobrowser

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// Window 是一个窗口句柄，所有方法都是"发一条 HTTP 指令"。
type Window struct {
	app *App
	id  string
}

// Window 取已存在的窗口（app.json 默认窗口 id 是 "main"）。
func (a *App) Window(id string) *Window { return &Window{app: a, id: id} }

// NewWindow 新建窗口，返回窗口 id。
func (a *App) NewWindow(opts map[string]any) (*Window, error) {
	if opts == nil {
		opts = map[string]any{}
	}
	data, err := a.do(http.MethodPost, "/v1/windows", opts)
	if err != nil {
		return nil, err
	}
	id, _ := data["id"].(string)
	if id == "" {
		id = "main"
	}
	return &Window{app: a, id: id}, nil
}

func (w *Window) call(action string, body any) error {
	_, err := w.app.do(http.MethodPost, "/v1/windows/"+w.id+"/"+action, body)
	return err
}

func (w *Window) ID() string { return w.id }

func (w *Window) Maximize() error   { return w.call("maximize", nil) }
func (w *Window) Unmaximize() error { return w.call("unmaximize", nil) }
func (w *Window) Minimize() error   { return w.call("minimize", nil) }
func (w *Window) Restore() error    { return w.call("restore", nil) }
func (w *Window) Show() error       { return w.call("show", nil) }
func (w *Window) Hide() error       { return w.call("hide", nil) }
func (w *Window) Focus() error      { return w.call("focus", nil) }
func (w *Window) Close() error      { return w.call("close", nil) }

func (w *Window) SetTitle(title string) error {
	_, err := w.app.do(http.MethodPost, "/v1/windows/"+w.id+"/title", map[string]any{"title": title})
	return err
}

func (w *Window) SetAlwaysOnTop(on bool) error {
	_, err := w.app.do(http.MethodPost, "/v1/windows/"+w.id+"/always-on-top", map[string]any{"on": on})
	return err
}

func (w *Window) SetFullScreen(on bool) error {
	_, err := w.app.do(http.MethodPost, "/v1/windows/"+w.id+"/fullscreen", map[string]any{"on": on})
	return err
}

// Bounds 窗口矩形。
type Bounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// SetBounds 改位置和大小。
func (w *Window) SetBounds(b Bounds) error {
	_, err := w.app.do(http.MethodPut, "/v1/windows/"+w.id+"/bounds", b)
	return err
}

// Navigate 让窗口跳到另一个地址。
func (w *Window) Navigate(url string) error {
	_, err := w.app.do(http.MethodPost, "/v1/windows/"+w.id+"/navigate", map[string]any{"url": url})
	return err
}

// Info 取窗口当前状态（标题/尺寸/是否最大化/是否加载完成）。
func (w *Window) Info() (map[string]any, error) {
	return w.app.do(http.MethodGet, "/v1/windows/"+w.id, nil)
}

// IsMaximized 便捷判断。
func (w *Window) IsMaximized() (bool, error) {
	i, err := w.Info()
	if err != nil {
		return false, err
	}
	v, _ := i["isMaximized"].(bool)
	return v, nil
}

// Capture 截一张窗口图存到 path（排查/自动化测试很好用）。
func (w *Window) Capture(path string) error {
	data, err := w.app.do(http.MethodPost, "/v1/windows/"+w.id+"/capture", nil)
	if err != nil {
		return err
	}
	b64, _ := data["pngBase64"].(string)
	if b64 == "" {
		return fmt.Errorf("外壳没有返回截图数据（size=%v bytes=%v，常见于虚拟机/远程桌面或窗口被完全遮挡）",
			data["size"], data["bytes"])
	}
	png, err := decodePNG(b64)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return os.WriteFile(path, png, 0o644)
}
