package gobrowser

import (
	"fmt"
	"net/http"
	"path/filepath"
)

// ---------------------------------------------------------------- 托盘

// MenuItem 托盘菜单项。Type 可为 "" / "separator" / "checkbox"，Action 可为 "show" / "quit"。
type MenuItem struct {
	ID       string `json:"id,omitempty"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type,omitempty"`
	Checked  bool   `json:"checked,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Action   string `json:"action,omitempty"`
}

// TrayOptions 托盘外观与菜单。
type TrayOptions struct {
	Icon    string     `json:"icon,omitempty"`
	Tooltip string     `json:"tooltip,omitempty"`
	Menu    []MenuItem `json:"menu,omitempty"`
}

// SetTray 创建或更新托盘（重复调用等于更新）。
//
// Icon 允许相对路径：这里统一转绝对路径。底座的工作目录是 app.json 所在目录（类库模式下是 %TEMP%），
// 相对路径传下去必然找不到，表现为"托盘图标静默不显示"。
func (a *App) SetTray(o TrayOptions) error {
	if o.Icon != "" {
		o.Icon = absPath(o.Icon)
	}
	_, err := a.do(http.MethodPost, "/v1/tray", o)
	return err
}

// DestroyTray 移除托盘图标。
func (a *App) DestroyTray() error {
	_, err := a.do(http.MethodPost, "/v1/tray/destroy", nil)
	return err
}

// SetTrayMenu 只更新菜单。
func (a *App) SetTrayMenu(items []MenuItem) error {
	_, err := a.do(http.MethodPost, "/v1/tray/menu", map[string]any{"menu": items})
	return err
}

// ---------------------------------------------------------------- 应用图标

// SetAppIcon 运行时替换"整个应用"的图标：所有窗口（标题栏 + 任务栏）+ 托盘 + macOS Dock。
//
// 典型用法是状态指示：空闲用 icon-idle.png，运行中用 icon-busy.png。
// 传空字符串恢复默认图标（app.json 的 window.icon，再不行用内置图标）。
//
// 和 Window.SetIcon 的区别：那个只改一个窗口，托盘和 Dock 不动。
// 只对之后创建的新窗口不生效（底座换图标时窗口已存在），新窗口请在建好后自己调一次。
func (a *App) SetAppIcon(iconPath string) error {
	body := map[string]any{"icon": ""}
	if iconPath != "" {
		body["icon"] = absPath(iconPath)
	}
	_, err := a.do(http.MethodPost, "/v1/icon", body)
	return err
}

// absPath 相对路径转绝对路径（失败时原样返回，让底座去报"文件不存在"）。
func absPath(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// ---------------------------------------------------------------- 通知

// Notify 弹系统通知。
func (a *App) Notify(title, body string) error {
	_, err := a.do(http.MethodPost, "/v1/notify", map[string]any{"title": title, "body": body})
	return err
}

// ---------------------------------------------------------------- 对话框

// FileFilter 文件类型过滤，例如 {"文本": []string{"txt","log"}}。
type FileFilter struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// FileDialogOptions 打开/保存对话框参数。
type FileDialogOptions struct {
	Title       string       `json:"title,omitempty"`
	DefaultPath string       `json:"defaultPath,omitempty"`
	Filters     []FileFilter `json:"filters,omitempty"`
	Properties  []string     `json:"properties,omitempty"` // openFile / openDirectory / multiSelections
}

// OpenFile 打开文件对话框，返回（路径列表, 是否取消）。
func (a *App) OpenFile(o FileDialogOptions) ([]string, bool, error) {
	d, err := a.do(http.MethodPost, "/v1/dialog/open", o)
	if err != nil {
		return nil, true, err
	}
	canceled, _ := d["canceled"].(bool)
	var paths []string
	if raw, ok := d["paths"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				paths = append(paths, s)
			}
		}
	}
	return paths, canceled, nil
}

// SaveFile 保存文件对话框，返回（路径, 是否取消）。
func (a *App) SaveFile(o FileDialogOptions) (string, bool, error) {
	d, err := a.do(http.MethodPost, "/v1/dialog/save", o)
	if err != nil {
		return "", true, err
	}
	canceled, _ := d["canceled"].(bool)
	p, _ := d["path"].(string)
	return p, canceled, nil
}

// MessageBoxOptions 消息框参数。Type: info / warning / error / question。
type MessageBoxOptions struct {
	Type      string   `json:"type,omitempty"`
	Title     string   `json:"title,omitempty"`
	Message   string   `json:"message,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Buttons   []string `json:"buttons,omitempty"`
	DefaultID int      `json:"defaultId,omitempty"`
}

// MessageBox 弹消息框，返回被点击按钮的下标。
func (a *App) MessageBox(o MessageBoxOptions) (int, error) {
	d, err := a.do(http.MethodPost, "/v1/dialog/message", o)
	if err != nil {
		return -1, err
	}
	if v, ok := d["response"].(float64); ok {
		return int(v), nil
	}
	return -1, fmt.Errorf("外壳没有返回 response")
}
