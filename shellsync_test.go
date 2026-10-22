package gobrowser

import (
	"os"
	"path/filepath"
	"testing"
)

// 准备一个假的 runtime 目录（只关心 resources/app.asar）。
func fakeRuntime(t *testing.T, asarContent string, marker string) string {
	t.Helper()
	dir := t.TempDir()
	res := filepath.Join(dir, "resources")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	if asarContent != "" {
		if err := os.WriteFile(filepath.Join(res, "app.asar"), []byte(asarContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if marker != "" {
		if err := os.WriteFile(filepath.Join(res, shellMarker), []byte(marker+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// 老 runtime（没有 shell.version）必须被更新，并且原件留 .bak。
func TestSyncShellUpgradesOldRuntime(t *testing.T) {
	dir := fakeRuntime(t, "OLD-ASAR", "")
	if err := syncShell(dir); err != nil {
		t.Fatalf("syncShell: %v", err)
	}

	res := filepath.Join(dir, "resources")
	got, err := os.ReadFile(filepath.Join(res, "app.asar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "OLD-ASAR" {
		t.Fatal("老底座没被替换")
	}
	if len(got) < 100 {
		t.Fatalf("写入的 asar 太小（%d 字节），不像真的打出来了", len(got))
	}
	if bak, err := os.ReadFile(filepath.Join(res, "app.asar.bak")); err != nil || string(bak) != "OLD-ASAR" {
		t.Fatalf("备份不对: %q err=%v", bak, err)
	}
	if v := readFileString(filepath.Join(res, shellMarker)); v != ShellVersion() {
		t.Fatalf("标记 = %q，期望 %q", v, ShellVersion())
	}
}

// runtime 自带底座比 SDK 新时，绝不能往回覆盖。
func TestSyncShellKeepsNewerRuntime(t *testing.T) {
	dir := fakeRuntime(t, "NEWER-ASAR", "9.9.9")
	if err := syncShell(dir); err != nil {
		t.Fatalf("syncShell: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "resources", "app.asar"))
	if string(got) != "NEWER-ASAR" {
		t.Fatal("把更新的运行时底座降级了")
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", "app.asar.bak")); err == nil {
		t.Fatal("没改动却产生了备份文件")
	}
}

// 已经同步过（标记相同）时是空操作：不重写、不备份。
func TestSyncShellNoopWhenSameVersion(t *testing.T) {
	dir := fakeRuntime(t, "SAME", ShellVersion())
	before, _ := os.Stat(filepath.Join(dir, "resources", "app.asar"))

	if err := syncShell(dir); err != nil {
		t.Fatalf("syncShell: %v", err)
	}
	after, _ := os.Stat(filepath.Join(dir, "resources", "app.asar"))
	if before.ModTime() != after.ModTime() {
		t.Fatal("同一版本不该重写 app.asar")
	}
}

// 内容已经一致（比如 SDK 之前写过、但标记丢了）时只补标记。
func TestSyncShellWritesMarkerOnly(t *testing.T) {
	data, _, err := shellBundle()
	if err != nil {
		t.Fatal(err)
	}
	dir := fakeRuntime(t, string(data), "")
	if err := syncShell(dir); err != nil {
		t.Fatalf("syncShell: %v", err)
	}
	res := filepath.Join(dir, "resources")
	if v := readFileString(filepath.Join(res, shellMarker)); v != ShellVersion() {
		t.Fatalf("标记没补上: %q", v)
	}
	// 内容相同 → 不该留下备份
	if _, err := os.Stat(filepath.Join(res, "app.asar.bak")); err == nil {
		t.Fatal("内容相同不该备份")
	}
}

// GOBROWSER_SHELL_SYNC=0 时不动任何文件（签名分发/只读场景）。
func TestSyncShellCanBeDisabled(t *testing.T) {
	t.Setenv("GOBROWSER_SHELL_SYNC", "0")
	dir := fakeRuntime(t, "OLD-ASAR", "")
	if err := syncShell(dir); err != nil {
		t.Fatalf("syncShell: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "resources", "app.asar"))
	if string(got) != "OLD-ASAR" {
		t.Fatal("关掉同步后仍被改写")
	}
}

func TestCmpShellVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.1.0", "1.0.9", 1},
		{"1.0.0", "1.0.0", 0},
		{"", "0.0.1", -1},
		{"1.2", "1.2.0", 0},
		{"v1.3.0", "1.2.9", 1},
		{"1.2.0-r2", "1.2.0", 0}, // 后缀忽略
		{"2", "10", -1},          // 数字比较，不是字典序
	}
	for _, c := range cases {
		if got := cmpShellVersion(c.a, c.b); got != c.want {
			t.Errorf("cmpShellVersion(%q,%q) = %d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}

// 内置底座必须能打出来且带 package.json（asar 头部合法），否则外壳起不来。
func TestEmbeddedShellBundle(t *testing.T) {
	data, ver, err := shellBundle()
	if err != nil {
		t.Fatalf("shellBundle: %v", err)
	}
	if len(data) < 200 {
		t.Fatalf("asar 太小: %d", len(data))
	}
	if len(data) < 16 || string(data[4:12]) == "" {
		t.Fatal("缺少 asar 头")
	}
	if ver == "" || ver == "0.0.0" {
		t.Fatalf("底座版本没读到: %q", ver)
	}
}
