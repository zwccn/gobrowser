package gobrowser

import (
	"os"
	"path/filepath"
)

// FindAppJSON 在 dir 里定位应用的 app.json，返回路径与是否找到。
//
// 查找顺序（第一个命中的胜出）：
//  1. <exeBase>.app.json —— 多产品放在同一个统一目录时的约定，避免互相覆盖
//  2. app.json           —— 单产品目录的旧约定，兼容
//
// exeBase 传空字符串（不知道该找哪个程序）时，目录里恰好只有一个
// <任意名>.app.json 才会采用它，否则退回 app.json。
func FindAppJSON(dir, exeBase string) (string, bool) {
	if exeBase != "" {
		if p := filepath.Join(dir, exeBase+".app.json"); fileExists(p) {
			return p, true
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.app.json")); len(matches) == 1 {
		return matches[0], true
	}
	if p := filepath.Join(dir, "app.json"); fileExists(p) {
		return p, true
	}
	return "", false
}

// CountAppJSON 数一下目录里有几个 <名字>.app.json（不含旧的 app.json）。
func CountAppJSON(dir string) int {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.app.json"))
	return len(matches)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
