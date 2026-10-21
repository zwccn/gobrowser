package gobrowser

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindAppJSON(t *testing.T) {
	t.Run("程序名优先于旧的 app.json", func(t *testing.T) {
		dir := t.TempDir()
		want := touch(t, dir, "Report.app.json")
		touch(t, dir, "app.json")
		got, ok := FindAppJSON(dir, "Report")
		if !ok || got != want {
			t.Fatalf("got %q ok=%v, want %q", got, ok, want)
		}
	})

	t.Run("只有旧的 app.json 时回退", func(t *testing.T) {
		dir := t.TempDir()
		want := touch(t, dir, "app.json")
		got, ok := FindAppJSON(dir, "Report")
		if !ok || got != want {
			t.Fatalf("got %q ok=%v, want %q", got, ok, want)
		}
	})

	t.Run("多产品目录里按程序名各取各的", func(t *testing.T) {
		dir := t.TempDir()
		a := touch(t, dir, "Report.app.json")
		b := touch(t, dir, "Other.app.json")
		if got, _ := FindAppJSON(dir, "Report"); got != a {
			t.Fatalf("Report 命中了 %q", got)
		}
		if got, _ := FindAppJSON(dir, "Other"); got != b {
			t.Fatalf("Other 命中了 %q", got)
		}
	})

	t.Run("不知道程序名且配置不唯一时不猜", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "Report.app.json")
		touch(t, dir, "Other.app.json")
		if _, ok := FindAppJSON(dir, ""); ok {
			t.Fatal("多个配置时不应该随便挑一个")
		}
		if n := CountAppJSON(dir); n != 2 {
			t.Fatalf("CountAppJSON=%d, want 2", n)
		}
	})

	t.Run("不知道程序名但只有一个配置时采用它", func(t *testing.T) {
		dir := t.TempDir()
		want := touch(t, dir, "Solo.app.json")
		if got, ok := FindAppJSON(dir, ""); !ok || got != want {
			t.Fatalf("got %q ok=%v, want %q", got, ok, want)
		}
	})

	t.Run("什么都没有", func(t *testing.T) {
		if _, ok := FindAppJSON(t.TempDir(), "Report"); ok {
			t.Fatal("空目录不应该命中")
		}
	})
}

func TestExeBaseName(t *testing.T) {
	cases := map[string]string{
		filepath.Join("opt", "app", "report"): "report",
		"report.exe":                          "report",
		"report":                              "report",
	}
	for in, want := range cases {
		if got := ExeBaseName(in); got != want {
			t.Errorf("ExeBaseName(%q)=%q, want %q", in, got, want)
		}
	}
}
