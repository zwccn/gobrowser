// relruntime：把官方（加固）Electron 平台 zip 变成可分发的 gobrowser 运行时 zip。
//
// 与 tools/pack 的区别：pack 处理"本机已有的 dist 目录"，本工具处理"跨平台官方 zip"，
// 直接产出可以直接解压成 runtime\ 的发布件，并保留原 zip 里的文件权限
// （Linux 的 gobrowser / chrome-sandbox 需要可执行位）。
//
//	go run ./tools/relruntime -zips C:\aitools\00.360易创 -app ./app \
//	    -out dist/release -version v16.0.9 -platforms win32-x64,linux-arm64
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zwccn/gobrowser/internal/asar"
)

// 支持的平台：官方 zip 名里的那一截
var known = []string{
	"win32-x64",
	"win32-ia32",
	"linux-x64",
	"linux-arm64",
	"linux-loong64",
	"linux-mips64el",
}

type result struct {
	platform string
	file     string
	size     int64
	sha256   string
}

// 官方平台名 → 清单里的 GOOS/GOARCH 键
var platformKeys = map[string]string{
	"win32-x64":      "windows/amd64",
	"win32-ia32":     "windows/386",
	"linux-x64":      "linux/amd64",
	"linux-arm64":    "linux/arm64",
	"linux-loong64":  "linux/loong64",
	"linux-mips64el": "linux/mips64le",
}

func main() {
	zips := flag.String("zips", "", "官方平台 zip 所在目录")
	appDir := flag.String("app", "./app", "底座 JS 源码目录")
	out := flag.String("out", "dist/release", "输出目录")
	version := flag.String("version", "v16.0.9", "运行时版本（用于文件名）")
	platforms := flag.String("platforms", strings.Join(known, ","), "要打包的平台，逗号分隔")
	dryRun := flag.Bool("n", false, "只检查，不写文件")
	manifestPath := flag.String("manifest", "", "同时生成运行时清单（SDK 自动获取用），通常指向 internal/runtimefetch/runtime-manifest.json")
	baseURL := flag.String("base", "", "清单里的下载前缀；默认 GitHub Release 地址")
	flag.Parse()

	if *zips == "" {
		fmt.Println("用法: relruntime -zips <官方 zip 目录> [-app ./app] [-out dist/release] [-version v16.0.9]")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail("创建输出目录失败: " + err.Error())
	}

	var done []result
	for _, p := range strings.Split(*platforms, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		src := filepath.Join(*zips, "electron-"+*version+"-"+p+".zip")
		if _, err := os.Stat(src); err != nil {
			fail("找不到 " + src)
		}
		dst := filepath.Join(*out, fmt.Sprintf("gobrowser-runtime-%s-%s.zip", *version, p))
		if *dryRun {
			st, _ := os.Stat(src)
			fmt.Printf("  · %-16s %s（源 %.1f MB）\n", p, dst, float64(st.Size())/1048576)
			continue
		}
		n, err := buildOne(src, dst, *appDir)
		if err != nil {
			fail(p + ": " + err.Error())
		}
		sum, _ := fileSHA256(dst)
		done = append(done, result{p, dst, n, sum})
		fmt.Printf("  ✓ %-16s %6.1f MB  %s\n", p, float64(n)/1048576, filepath.Base(dst))
	}

	if *dryRun {
		return
	}
	if *manifestPath != "" {
		if err := writeManifest(*manifestPath, *version, *baseURL, done); err != nil {
			fail("生成清单失败: " + err.Error())
		}
		fmt.Printf("  ✓ 清单已更新 %s\n", *manifestPath)
	}
	fmt.Println()
	for _, d := range done {
		fmt.Printf("%-16s %s\n", d.platform, d.file)
	}
}

// writeManifest 生成 SDK 用的运行时清单（含 size 与 sha256）。
func writeManifest(path, version, base string, rs []result) error {
	if base == "" {
		base = fmt.Sprintf("https://github.com/zwccn/gobrowser/releases/download/runtime-%s/", version)
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	type entry struct {
		File   string `json:"file"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	m := struct {
		Version   string           `json:"version"`
		Release   string           `json:"release"`
		Base      string           `json:"base"`
		Platforms map[string]entry `json:"platforms"`
	}{
		Version:   version,
		Release:   "runtime-" + version,
		Base:      base,
		Platforms: map[string]entry{},
	}
	for _, r := range rs {
		key, ok := platformKeys[r.platform]
		if !ok {
			return fmt.Errorf("未知平台 %s", r.platform)
		}
		m.Platforms[key] = entry{File: filepath.Base(r.file), Size: r.size, SHA256: r.sha256}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func buildOne(srcZip, dstZip, appDir string) (int64, error) {
	zr, err := zip.OpenReader(srcZip)
	if err != nil {
		return 0, err
	}
	defer zr.Close()

	// 先确认里面哪个是主程序：windows 是 electron.exe，其余是 electron
	var mainName string
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base == "electron.exe" || base == "electron" {
			mainName = f.Name
			break
		}
	}
	if mainName == "" {
		return 0, fmt.Errorf("zip 里没有 electron/electron.exe")
	}
	base := filepath.Base(mainName) // electron 或 electron.exe
	newMain := strings.TrimSuffix(mainName, base) + strings.Replace(base, "electron", "gobrowser", 1)

	out, err := os.Create(dstZip)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	zw := zip.NewWriter(out)

	entries := append([]*zip.File(nil), zr.File...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	for _, f := range entries {
		// 先把可能存在的旧 app.asar 丢掉，稍后写我们自己的
		if f.Name == "resources/app.asar" {
			continue
		}
		name := f.Name
		if name == mainName {
			name = newMain
		}
		if err := copyEntry(zw, f, name); err != nil {
			return 0, fmt.Errorf("写 %s 失败: %w", name, err)
		}
	}

	// 注入底座：先把 app 目录临时打成 asar，再作为 zip 条目写入
	tmp, err := os.CreateTemp("", "gobrowser-app-*.asar")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if err := asar.Write(appDir, tmpPath); err != nil {
		return 0, fmt.Errorf("生成 app.asar 失败: %w", err)
	}
	if err := addFile(zw, "resources/app.asar", tmpPath); err != nil {
		return 0, err
	}

	if err := zw.Close(); err != nil {
		return 0, err
	}
	st, err := os.Stat(dstZip)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// copyEntry 按原始权限/时间复制一个条目（改名可选）。
func copyEntry(zw *zip.Writer, f *zip.File, name string) error {
	hdr := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: f.Modified,
		Comment:  f.Comment,
	}
	if hdr.Modified.IsZero() {
		hdr.Modified = time.Now()
	}
	// 保留原 zip 记录的权限位（Linux 可执行位就靠它）
	if mode := f.Mode(); mode != 0 {
		hdr.SetMode(mode)
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	if f.FileInfo().IsDir() {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(w, rc)
	return err
}

func addFile(zw *zip.Writer, name, path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(st)
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.Method = zip.Deflate
	hdr.SetMode(0o644)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}

func fail(msg string) {
	fmt.Println("✗ " + msg)
	os.Exit(1)
}
