package gobrowser

import "testing"

func TestAutoFetchEnabled(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{"", false},       // 测试进程不是 go run，默认关闭
		{"1", true},       // 显式开启
		{"true", true},    //
		{"0", false},      // 显式关闭
		{"off", false},    //
		{"no", false},     //
		{"  yes  ", true}, // 容忍空白与大小写
	}
	for _, c := range cases {
		t.Setenv("GOBROWSER_AUTO_FETCH", c.env)
		if got := autoFetchEnabled(); got != c.want {
			t.Errorf("GOBROWSER_AUTO_FETCH=%q → %v, want %v", c.env, got, c.want)
		}
	}
}

func TestIsGoRun(t *testing.T) {
	// go test 生成的二进制包路径是模块路径，不该被判定成 go run
	if isGoRun() {
		t.Error("go test 的二进制被误判为 go run")
	}
}
