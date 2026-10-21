// Package gobrowser 用 Go 起一个真正的桌面窗口：初始化、Show，就完了。
//
// # 三行出窗口
//
//	package main
//
//	import "github.com/zwccn/gobrowser"
//
//	func main() {
//		gobrowser.New(gobrowser.Config{Title: "报表中心"}).Show()
//	}
//
// # 换成自己的页面
//
// 页面就是普通网页，三种来源任选：
//
//	gobrowser.New(cfg).ShowDir("./web")                       // 本地目录
//	gobrowser.New(cfg).ShowFS(sub)                            // go:embed 之后 fs.Sub
//	gobrowser.New(cfg).ShowURL("http://127.0.0.1:8099")       // 已有 HTTP 服务
//
// 业务接口可以和页面共用一个服务，不用额外端口：
//
//	gobrowser.New(cfg).
//		HandleFunc("/api/list", listHandler).
//		ShowDir("./web")
//
// # 精细控制
//
// 需要拿到窗口句柄时用不阻塞的 Start：
//
//	app := gobrowser.New(gobrowser.Config{Title: "报表中心", Frameless: true})
//	app.Handle("/", http.FileServer(http.Dir("./web")))
//	if err := app.Start(); err != nil {
//		gobrowser.Fatal("com.acme.report", err)
//	}
//	app.Window().SetTitle("已连接")
//	app.Window().Maximize()
//	app.Wait()
//
// # 运行时从哪里来
//
// 窗口由一份 Electron 运行时（同目录的 runtime\）托管，它不属于 Go 进程：
// 任务管理器里窗口的宿主是 gobrowser.exe，不是你的程序。
//
// 查找顺序：同级 runtime/ → exe 尾部内嵌载荷（首次运行解压到用户缓存）→ 报错。
// 运行时不在 Go 模块里，需要单独分发，见项目 README。
//
// # 平台
//
// Windows amd64/386、Linux amd64/arm64/loong64/mips64le、macOS amd64/arm64
// 均可交叉编译（CGO_ENABLED=0，产物纯静态）。
package gobrowser
