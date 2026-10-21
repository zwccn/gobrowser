//go:build !windows

package gobrowser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// showErrorBox 在 Linux/macOS 上尽量弹一个原生提示，弹不出来就只打 stderr。
func showErrorBox(title, text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("osascript", "-e",
			fmt.Sprintf(`display alert %q message %q as critical`, title, text))
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command("zenity", "--error", "--title", title, "--text", text)
		} else if _, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.Command("kdialog", "--title", title, "--error", text)
		} else if _, err := exec.LookPath("xmessage"); err == nil {
			cmd = exec.Command("xmessage", "-center", title+": "+text)
		}
	}
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "[webshell] %s: %s\n", title, text)
		return
	}
	_ = cmd.Start()
}
