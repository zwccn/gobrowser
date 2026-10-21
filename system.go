package gobrowser

import (
	"fmt"
	"net/http"
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
func (a *App) SetTray(o TrayOptions) error {
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
