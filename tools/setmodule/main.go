// setmodule：把模块路径改成准备发布的地址（一次性操作）。
//
//	go run ./tools/setmodule -m github.com/you/gobrowser
//
// 会做三件事：改 go.mod 的 module、改所有 .go 文件的 import 前缀、gofmt。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	module := flag.String("m", "", "新的模块路径，例如 github.com/you/gobrowser")
	flag.Parse()
	if *module == "" || !strings.Contains(*module, "/") {
		fmt.Println("用法: setmodule -m github.com/you/gobrowser")
		os.Exit(2)
	}

	old := currentModule()
	if old == *module {
		fmt.Println("模块路径已经是", old, "，无需修改")
		return
	}

	// go.mod
	gm, err := os.ReadFile("go.mod")
	must(err)
	must(os.WriteFile("go.mod", []byte(strings.Replace(string(gm), "module "+old, "module "+*module, 1)), 0o644))

	// 所有 .go 文件
	n := 0
	err = filepath.Walk(".", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s, changed := rewriteImports(string(b), old, *module)
		if !changed {
			return nil
		}
		n++
		return os.WriteFile(p, []byte(s), 0o644)
	})
	must(err)

	fmt.Printf("✓ 模块路径 %s → %s（改了 %d 个文件）\n", old, *module, n)
	if _, err := exec.LookPath("gofmt"); err == nil {
		_ = exec.Command("gofmt", "-w", ".").Run()
	}
	fmt.Println("下一步: go build ./... && go test ./... && git tag v0.1.0 && git push --tags")
}

// rewriteImports 只改"看起来是 import 规格"的行，避免误伤同名的字符串字面量
// （例如运行时二进制名 "gobrowser" 这种普通常量）。
func rewriteImports(src, old, newMod string) (string, bool) {
	lines := strings.Split(src, "\n")
	changed := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") {
			continue // 注释里的示例不动
		}
		// import 规格的形态：`"pkg"` 或 `alias "pkg"`；也用括号形式的包路径
		spec := t
		if j := strings.Index(spec, `"`); j >= 0 {
			spec = spec[j:] // 去掉可能的别名前缀
		}
		if !strings.HasPrefix(spec, `"`+old) {
			continue
		}
		lines[i] = strings.Replace(line, `"`+old, `"`+newMod, 1)
		changed = true
	}
	return strings.Join(lines, "\n"), changed
}

func currentModule() string {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		fmt.Println("找不到 go.mod，请在模块根目录执行")
		os.Exit(2)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	fmt.Println("go.mod 里没有 module 行")
	os.Exit(2)
	return ""
}

func must(err error) {
	if err != nil {
		fmt.Println("✗", err)
		os.Exit(1)
	}
}
