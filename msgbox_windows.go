//go:build windows

package gobrowser

import (
	"syscall"
	"unsafe"
)

var (
	user32       = syscall.NewLazyDLL("user32.dll")
	procMessageB = user32.NewProc("MessageBoxW")
)

const (
	mbIconError     = 0x00000010
	mbTopmost       = 0x00040000
	mbSetForeground = 0x00010000
)

// showErrorBox 在 Windows 上弹一个原生错误框（不需要 cgo）。
func showErrorBox(title, text string) {
	t, err1 := syscall.UTF16PtrFromString(text)
	c, err2 := syscall.UTF16PtrFromString(title)
	if err1 != nil || err2 != nil {
		return
	}
	procMessageB.Call(0,
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(c)),
		uintptr(mbIconError|mbTopmost|mbSetForeground))
}
