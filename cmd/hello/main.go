// 最小示例：引用类库 → 初始化 → Show。就这三行。
//
//	go run ./cmd/hello            # 内置欢迎页
//	go run ./cmd/hello -dir web   # 换成自己的页面目录
package main

import (
	"flag"
	"os"
	"time"

	"github.com/zwccn/gobrowser"
)

func main() {
	dir := flag.String("dir", "", "页面目录（留空则显示内置欢迎页）")
	hold := flag.Duration("hold", 0, "运行多久后自动关闭（0 = 一直开着，仅调试用）")
	flag.Parse()

	app := gobrowser.New(gobrowser.Config{
		Title:     "我的第一个窗口",
		Width:     1100,
		Height:    720,
		Frameless: false,
		GPU:       "disable", // 虚拟机/远程桌面友好
	})

	if *hold > 0 {
		go func() {
			for app.App() == nil {
				time.Sleep(50 * time.Millisecond)
			}
			time.Sleep(*hold)
			_ = app.Quit()
		}()
	}

	var err error
	if *dir != "" {
		err = app.ShowDir(*dir)
	} else {
		err = app.Show()
	}
	if err != nil {
		gobrowser.Fatal("local.hello", err)
		os.Exit(2)
	}
}
