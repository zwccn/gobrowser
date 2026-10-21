// pack：把一份官方（加固版）Electron dist 变成 WebShell 运行时目录。
//
//  1. 整份拷贝到 -out
//
//  2. electron(.exe) 改名为 webshell(.exe) —— 只改名，不动一个字节
//
//  3. 把底座 JS（-app）打成 resources/app.asar
//
//  4. 校验 electron.sign / lic / resources/app.asar
//
//     go run ./tools/pack -dist <原版 dist> -out <runtime 目录> -app ./app
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zwccn/gobrowser/internal/asar"
)

func main() {
	dist := flag.String("dist", "", "源 Electron dist 目录")
	out := flag.String("out", "", "输出 runtime 目录")
	appDir := flag.String("app", "./app", "底座 JS 源码目录")
	flag.Parse()

	if *dist == "" || *out == "" {
		fmt.Println("用法: pack -dist <electron dist> -out <runtime dir> [-app ./app]")
		os.Exit(2)
	}
	must(copyDir(*dist, *out))

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	src := filepath.Join(*out, "electron"+ext)
	dst := filepath.Join(*out, "gobrowser"+ext)
	if _, err := os.Stat(src); err == nil {
		must(os.Rename(src, dst))
		fmt.Println("✓ 改名:", filepath.Base(dst))
	} else if _, err := os.Stat(dst); err != nil {
		fail("dist 里既没有 electron" + ext + " 也没有 gobrowser" + ext)
	}

	asarPath := filepath.Join(*out, "resources", "app.asar")
	must(asar.Write(*appDir, asarPath))
	if st, err := os.Stat(asarPath); err == nil {
		fmt.Printf("✓ 生成: resources/app.asar (%d B)\n", st.Size())
	}

	for _, rel := range []string{"gobrowser" + ext, "electron.sign", "lic", filepath.Join("resources", "app.asar")} {
		if _, err := os.Stat(filepath.Join(*out, rel)); err != nil {
			fail("校验失败，缺少 " + rel)
		}
	}
	fmt.Println("✓ 校验通过: gobrowser" + ext + " / electron.sign / lic / resources/app.asar")
	fmt.Println("→ 运行时目录:", *out)
}

func fail(msg string) { fmt.Println("✗ " + msg); os.Exit(1) }
func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func copyDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		outf, err := os.Create(target)
		if err != nil {
			return err
		}
		defer outf.Close()
		_, err = io.Copy(outf, in)
		return err
	})
}
