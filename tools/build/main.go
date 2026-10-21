// build：交叉编译 + 组装发布目录 + 打 zip + 生成安装包脚本。
//
//	go run ./tools/build -targets win-x64 -bundle bundled -runtime-dir C:\tmp_ws\runtime
//	go run ./tools/build -targets win-x64,linux-arm64 -bundle shared
//	go run ./tools/build -targets linux-arm64 -bundle bundled -zips C:\aitools\00.360易创
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
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/zwccn/gobrowser/internal/asar"
	"github.com/zwccn/gobrowser/internal/payloadfmt"
)

type target struct {
	name    string
	goos    string
	goarch  string
	zipName string // 对应的官方 electron 包名
	ext     string
}

var targetTable = map[string]target{
	"win-x64":        {"win-x64", "windows", "amd64", "electron-v16.0.9-win32-x64.zip", ".exe"},
	"win-x86":        {"win-x86", "windows", "386", "electron-v16.0.9-win32-ia32.zip", ".exe"},
	"linux-x64":      {"linux-x64", "linux", "amd64", "electron-v16.0.9-linux-x64.zip", ""},
	"linux-arm64":    {"linux-arm64", "linux", "arm64", "electron-v16.0.9-linux-arm64.zip", ""},
	"linux-loong64":  {"linux-loong64", "linux", "loong64", "electron-v16.0.9-linux-loong64.zip", ""},
	"linux-mips64el": {"linux-mips64el", "linux", "mips64le", "electron-v16.0.9-linux-mips64el.zip", ""},
}

func main() {
	appdir := flag.String("appdir", ".", "Go 项目目录（含 go.mod）")
	pkg := flag.String("pkg", "./cmd/demo", "要编译的 main 包")
	name := flag.String("name", "demo", "产物名（exe 文件名）")
	appJSON := flag.String("app", "", "app.json 路径（默认 <pkg>/app.json）")
	shellJS := flag.String("shell-js", "./app", "底座 JS 目录（bundled 时打进 app.asar）")
	tgts := flag.String("targets", "win-x64", "逗号分隔：win-x64 / win-x86 / linux-x64 / linux-arm64 / linux-loong64 / linux-mips64el")
	bundle := flag.String("bundle", "bundled", "bundled=发布目录带 runtime；shared=不带")
	runtimeDir := flag.String("runtime-dir", "", "本机平台现成的 runtime 目录（bundled 时直接复制）")
	zips := flag.String("zips", "", "官方 electron zip 目录（bundled 跨平台时解压）")
	icon := flag.String("icon", "", "Windows 图标（需 go-winres）")
	out := flag.String("out", "dist", "输出目录")
	mkISS := flag.Bool("iss", false, "额外生成 Inno Setup 脚本")
	single := flag.Bool("single", false, "单文件模式：把 runtime 压成 zip 追加到 exe 尾部（首次运行自动释放）")
	flag.Parse()

	if *appJSON == "" {
		*appJSON = filepath.Join(*appdir, filepath.FromSlash(*pkg), "app.json")
	}
	ver := appVersion(*appJSON)
	fmt.Println("WebShell build ·", *name, "v"+ver)

	var done []string
	for _, raw := range strings.Split(*tgts, ",") {
		t, ok := targetTable[strings.TrimSpace(raw)]
		if !ok {
			fail("不支持的目标 %q（可用: %s）", raw, strings.Join(keys(), ", "))
		}
		if err := buildOne(*appdir, *pkg, *name, *appJSON, *shellJS, *out, *bundle, *runtimeDir, *zips, *icon, ver, *single, t); err != nil {
			fail("%s: %v", t.name, err)
		}
		done = append(done, t.name)
	}

	if *mkISS {
		p := filepath.Join(*out, *name+".iss")
		if err := writeISS(p, *name, ver, done[0]); err != nil {
			fail("生成 iss 失败: %v", err)
		}
		fmt.Println("\n✓ 安装包脚本:", p, "（用 Inno Setup 6 编译）")
	}
	fmt.Println("\n完成:", strings.Join(done, ", "))
}

func buildOne(appdir, pkg, name, appJSON, shellJS, out, bundle, runtimeDir, zips, icon, ver string, single bool, t target) error {
	dir := filepath.Join(out, t.name)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if t.goos == "windows" && icon != "" {
		if err := genSyso(appdir, pkg, icon, name, ver); err != nil {
			fmt.Println("  ! 图标未生成:", err)
		}
	}

	// 交叉编译
	exePath := filepath.Join(dir, name+t.ext)
	ld := "-s -w"
	if t.goos == "windows" {
		ld = "-H windowsgui " + ld // GUI 子系统：双击不弹控制台
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ld, "-o", exePath, pkg)
	cmd.Dir = appdir
	cmd.Env = append(os.Environ(), "GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("编译失败: %w", err)
	}
	if st, err := os.Stat(exePath); err == nil {
		fmt.Printf("  ✓ %-14s %s  %.2f MB\n", t.name, name+t.ext, float64(st.Size())/1024/1024)
	}

	if _, statErr := os.Stat(appJSON); statErr == nil {
		if err := copyFile(appJSON, filepath.Join(dir, "app.json")); err != nil {
			return err
		}
	} else {
		fmt.Printf("  · %-14s 无 app.json（由类库自动生成，跳过）\n", t.name)
	}
	if icon != "" {
		_ = copyFile(icon, filepath.Join(dir, filepath.Base(icon)))
	}

	if single {
		// 单文件：runtime 压成 zip 追加到 exe 尾部，目录里只留 exe + app.json
		tmp := filepath.Join(os.TempDir(), "ws-payload-"+t.name)
		_ = os.RemoveAll(tmp)
		defer os.RemoveAll(tmp)
		if err := prepareRuntimeDir(tmp, runtimeDir, zips, shellJS, t); err != nil {
			return err
		}
		payloadZip := tmp + ".zip"
		defer os.Remove(payloadZip)
		if err := zipDir(tmp, payloadZip); err != nil {
			return err
		}
		tr, err := payloadfmt.Append(exePath, payloadZip)
		if err != nil {
			return fmt.Errorf("附加载荷失败: %w", err)
		}
		if st, err := os.Stat(exePath); err == nil {
			fmt.Printf("  ✓ %-14s 单文件 %.1f MB（载荷 %.1f MB，%s）\n", t.name,
				float64(st.Size())/1024/1024, float64(tr.Size)/1024/1024, tr.Describe())
		}
	} else if bundle == "bundled" {
		rtDir := filepath.Join(dir, "runtime")
		if err := prepareRuntimeDir(rtDir, runtimeDir, zips, shellJS, t); err != nil {
			return err
		}
		fmt.Printf("  ✓ %-14s runtime 就位\n", t.name)
	}

	zipPath := filepath.Join(out, name+"-"+t.name+".zip")
	if err := zipDir(dir, zipPath); err != nil {
		return err
	}
	sum, _ := sha256File(zipPath)
	st, _ := os.Stat(zipPath)
	fmt.Printf("  ✓ %-14s %s  %.1f MB  sha256=%s…\n", t.name, filepath.Base(zipPath),
		float64(st.Size())/1024/1024, sum[:16])
	return nil
}

// prepareRuntimeDir 按目标平台准备一份 runtime 目录（复制或解压官方 zip）。
func prepareRuntimeDir(dst, runtimeDir, zips, shellJS string, t target) error {
	fromLocal := runtimeDir != "" && t.goos == runtime.GOOS && t.goarch == runtime.GOARCH
	switch {
	case fromLocal:
		return copyDir(runtimeDir, dst)
	case zips != "":
		z := filepath.Join(zips, t.zipName)
		if _, err := os.Stat(z); err != nil {
			return fmt.Errorf("找不到 %s（可用 -runtime-dir 指定本机平台目录）", z)
		}
		return unpackRuntime(z, dst, shellJS)
	default:
		return fmt.Errorf("需要 -runtime-dir（本机平台）或 -zips（任意平台）")
	}
}

// unpackRuntime 解压官方 electron zip → 改名 electron→webshell → 塞底座 asar。
func unpackRuntime(zipPath, dst, shellJS string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		p := filepath.Join(dst, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(p, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		outf, err := os.Create(p)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(outf, rc)
		outf.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	// 改名：electron / electron.exe → webshell / webshell.exe（zip 里没有扩展名的就是 Linux）
	for _, name := range []string{"electron", "electron.exe"} {
		old := filepath.Join(dst, name)
		if _, err := os.Stat(old); err == nil {
			newName := "gobrowser" + strings.TrimPrefix(name, "electron")
			_ = os.Remove(filepath.Join(dst, newName))
			if err := os.Rename(old, filepath.Join(dst, newName)); err != nil {
				return err
			}
			break
		}
	}

	// 底座代码
	return asar.Write(shellJS, filepath.Join(dst, "resources", "app.asar"))
}

func genSyso(appdir, pkg, icon, name, ver string) error {
	if _, err := exec.LookPath("go-winres"); err != nil {
		return fmt.Errorf("未安装 go-winres（go install github.com/tc-hib/go-winres@latest）")
	}
	args := []string{"simply",
		"--icon", icon,
		"--product-name", name,
		"--file-description", name,
		"--file-version", ver,
		"--product-version", ver,
	}
	cmd := exec.Command("go-winres", args...)
	cmd.Dir = filepath.Join(appdir, filepath.FromSlash(pkg))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	fmt.Printf("  ✓ %-14s rsrc_windows_*.syso\n", "win")
	return nil
}

func appVersion(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "1.0.0"
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &v) != nil || v.Version == "" {
		return "1.0.0"
	}
	return v.Version
}

func writeISS(path, name, ver, dirName string) error {
	body := fmt.Sprintf(`; 由 webshell build -iss 生成，用 Inno Setup 6 打开编译
#define MyAppName "%[1]s"
#define MyAppVersion "%[2]s"
#define MyAppExeName "%[1]s.exe"

[Setup]
AppId={{8C1E4D6A-3F7B-4E52-9A0C-%[4]s}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
OutputBaseFilename=%[1]s-%[2]s-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64

[Languages]
Name: "chinese"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Files]
Source: "%[3]s\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务:"

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "立即启动"; Flags: nowait postinstall skipifsilent
`, name, ver, dirName, guidTail(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func guidTail(s string) string {
	h := sha256.Sum256([]byte(s))
	x := strings.ToUpper(hex.EncodeToString(h[:4]))
	return x[0:4] + "-" + x[4:8]
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	outf, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer outf.Close()
	_, err = io.Copy(outf, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(t, 0o755)
		}
		return copyFile(p, t)
	})
}

type zipItem struct {
	path string
	info os.FileInfo
}

func zipDir(src, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	var items []zipItem
	if err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		items = append(items, zipItem{p, info})
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	for _, it := range items {
		rel, _ := filepath.Rel(src, it.path)
		hdr, err := zip.FileInfoHeader(it.info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		in, err := os.Open(it.path)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, in)
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func sha256File(p string) (string, error) {
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

func keys() []string {
	var ks []string
	for k := range targetTable {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func fail(format string, args ...any) {
	fmt.Printf("✗ "+format+"\n", args...)
	os.Exit(1)
}
