package gobrowser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeInfo 描述一份被选中的运行时。
type RuntimeInfo struct {
	Dir        string // runtime 目录
	Exe        string // gobrowser(.exe) 全路径
	Version    string // 来自 runtime/version 文件
	Electron   string // 来自握手，如 16.0.9
	Browser360 string // 来自握手，如 10.6.1001.11
}

// 运行时必须齐备的最小文件集（缺一项即视为无效）
var requiredFiles = []string{
	"electron.sign",
	filepath.Join("resources", "app.asar"),
}

// FindRuntime 以"当前可执行文件所在目录"为基准找运行时。
func FindRuntime(preferBundled bool) (RuntimeInfo, error) {
	exe, err := os.Executable()
	if err != nil {
		return RuntimeInfo{}, err
	}
	return FindRuntimeAt(filepath.Dir(exe), preferBundled)
}

// FindRuntimeAt 只找同级目录：
//  1. <dir>\..\runtime   （与程序所在目录同级，多个产品共用一份）
//  2. <dir>\runtime      （程序内部自带）
//
// preferBundled=true 时颠倒顺序（自带版优先用自己的）。
func FindRuntimeAt(dir string, preferBundled bool) (RuntimeInfo, error) {
	exeDir, err := filepath.Abs(dir)
	if err != nil {
		return RuntimeInfo{}, err
	}
	bundled := filepath.Join(exeDir, "runtime")
	sibling := filepath.Join(filepath.Dir(exeDir), "runtime")

	candidates := []string{sibling, bundled}
	if preferBundled {
		candidates = []string{bundled, sibling}
	}

	var problems []string
	for _, d := range candidates {
		rt, missing := Probe(d)
		if len(missing) == 0 {
			rt.Version = readVersion(d)
			return rt, nil
		}
		if _, statErr := os.Stat(d); statErr == nil {
			problems = append(problems, fmt.Sprintf("%s 缺少 %s", d, strings.Join(missing, ", ")))
		} else {
			problems = append(problems, fmt.Sprintf("%s 不存在", d))
		}
	}

	return RuntimeInfo{}, fmt.Errorf(
		"未找到可用运行时\n"+
			"  程序目录: %s\n"+
			"  尝试路径:\n    - %s\n    - %s\n"+
			"  详情: %s\n"+
			"  处理建议: 把 runtime 目录放到程序同级目录（与本程序在同一父目录下）",
		exeDir, candidates[0], candidates[1], strings.Join(problems, "；"))
}

// Probe 检查某个目录是否是一个完整运行时，返回它和缺失项。
func Probe(dir string) (RuntimeInfo, []string) {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return RuntimeInfo{}, []string{"目录"}
	}
	var missing []string
	exe := ""
	for _, name := range []string{"gobrowser" + platExt(), "electron" + platExt()} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			exe = p
			break
		}
	}
	if exe == "" {
		missing = append(missing, "gobrowser"+platExt())
	}
	for _, rel := range requiredFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			missing = append(missing, rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "lic")); err != nil {
		missing = append(missing, "lic/")
	}
	if len(missing) > 0 {
		return RuntimeInfo{}, missing
	}
	return RuntimeInfo{Dir: dir, Exe: exe}, nil
}

func readVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func platExt() string {
	if isWindows() {
		return ".exe"
	}
	return ""
}
