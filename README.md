# gobrowser

[![CI](https://github.com/zwccn/gobrowser/actions/workflows/ci.yml/badge.svg)](https://github.com/zwccn/gobrowser/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zwccn/gobrowser.svg)](https://pkg.go.dev/github.com/zwccn/gobrowser)
[![Go Report Card](https://goreportcard.com/badge/github.com/zwccn/gobrowser)](https://goreportcard.com/report/github.com/zwccn/gobrowser)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**用 Go 起一个真正的桌面窗口：初始化、Show，就完了。**

```go
import "github.com/zwccn/gobrowser"

gobrowser.New(gobrowser.Config{Title: "报表中心"}).Show()
```

- 窗口由底层 Electron 运行时托管，**运行时一个字节都没改**（只新增一个 19 KB 的 `app.asar`）
- 页面就是你熟悉的网页：目录、`embed.FS`、或已有的 HTTP 服务
- 窗口控制走本地 HTTP（Go 侧）/ preload 桥（页面侧），两种语言都不用碰 Electron
- 支持 Windows x64/x86、Linux x64/arm64/loong64/mips64el

> 设计细节见 [架构方案](docs/架构方案.md)，接入细节见 [开发者指南](docs/开发者指南.md)，已知差距与待办见 [TODO](docs/TODO.md)。



## 安装

```bash
go get github.com/zwccn/gobrowser
```

### 运行时不在 Go 模块里（但不用手动折腾）

`go get` 只拿到 SDK（约 200 KB）。真正画窗口的是一份 Electron 运行时（80–105 MB）。
三种拿法，任选：

| 场景 | 做法 |
|---|---|
| **开发（推荐，零手动）** | 什么都不用做。`go run .` 发现没有运行时，会自动下载到 `%LOCALAPPDATA%\gobrowser\runtimes\`，同一台机器只下一次 |
| **打包** | `go run ./tools/build -targets win-x64 -bundle bundled -fetch …` —— 自动下载并塞进发行目录 |
| **离线 / 手动** | 从 [Releases](https://github.com/zwccn/gobrowser/releases/tag/runtime-v16.0.9) 下对应平台 zip，解压成程序同级的 `runtime\` |

自动获取只发生在 `go run`（或你显式设了 `GOBROWSER_AUTO_FETCH=1`）；
**`go build` 出来的程序永远不会自己联网下载**，缺运行时就是报错，部署行为不变。

国内网络 / 企业内网 / 离线机器：

```bash
GOBROWSER_RUNTIME_BASE=https://你的镜像/       # 换下载源
GOBROWSER_RUNTIME_DIR=D:\shared\runtime-zips   # 直接读本地 zip 目录，完全不联网
GOBROWSER_RUNTIME_CACHE=D:\cache               # 换缓存位置
GOBROWSER_AUTO_FETCH=0                         # 彻底关闭自动获取
```

手动下载时，对应关系：

| 你编译的目标 | 下载 |
|---|---|
| `windows/amd64` | `gobrowser-runtime-v16.0.9-win32-x64.zip` |
| `windows/386` | `gobrowser-runtime-v16.0.9-win32-ia32.zip` |
| `linux/amd64` | `gobrowser-runtime-v16.0.9-linux-x64.zip` |
| `linux/arm64` | `gobrowser-runtime-v16.0.9-linux-arm64.zip` |
| `linux/loong64` | `gobrowser-runtime-v16.0.9-linux-loong64.zip` |
| `linux/mips64le` | `gobrowser-runtime-v16.0.9-linux-mips64el.zip` |

解压后的目录必须是这个样子：

```
你的程序\
├── 你的程序.exe
├── runtime\                    ← 渲染层，必须完整
│   ├── gobrowser.exe           ← 渲染进程（Go 程序是它的父进程）
│   ├── electron.sign
│   ├── lic\
│   └── resources\app.asar      ← 底座代码，19 KB
└── web\                        ← 你的页面
```

缺任何一项都会**直接报错并说明缺哪个文件**（写日志 + 弹原生窗口），不会白屏。

想做成"一个 exe 搞定"，用 `-single` 形态把运行时追加到自己 exe 尾部，首次运行自动释放到用户缓存。

---

## 0. 三行出窗口

```go
package main

import "github.com/zwccn/gobrowser"

func main() {
    gobrowser.New(gobrowser.Config{Title: "报表中心"}).Show()
}
```

`Show()` 显示内置欢迎页；换成自己的页面：

```go
app := gobrowser.New(gobrowser.Config{Title: "报表中心", Width: 1200, Height: 800, Frameless: true})

app.ShowDir("./web")              // 本地目录
app.ShowFS(sub)                   // //go:embed web 之后 fs.Sub
app.ShowURL("http://127.0.0.1:8099")  // 页面在别的服务上
app.HandleFunc("/api/list", list).ShowDir("./web")   // 顺手挂业务 API（和 UI 同一个服务）
```

`Config` 常用字段（其余都有默认值）：

| 字段 | 说明 | 默认 |
|---|---|---|
| `Title` | 窗口标题 / 托盘名 | exe 文件名 |
| `Width` / `Height` | 窗口尺寸 | 1024 × 720 |
| `MinWidth` / `MinHeight` | 最小尺寸 | — |
| `Frameless` | 无边框（页面自己画标题栏） | false |
| `Resizable` / `AlwaysOnTop` | 可缩放 / 置顶 | true / false |
| `Icon` | 窗口与托盘图标 | 内置图标 |
| `GPU` | `"disable"` 关硬件加速（虚拟机/RDP） | 自动 |
| `CloseToTray` | 点关闭收进托盘 | false |
| `Tray` | 托盘图标 + 菜单 | 不创建 |
| `OnEvent` | 收低频事件 | — |

需要更细的控制时用非阻塞的 `Start()`（先注册路由，再启动），之后用 `Window()` / `App()` / `Wait()`：

```go
app := gobrowser.New(gobrowser.Config{Title: "报表中心", Frameless: true})
app.Handle("/", http.FileServer(http.Dir("./web")))   // 页面
app.HandleFunc("/api/list", list)                     // 业务接口（同一个服务）
if err := app.Start(); err != nil {
    gobrowser.Fatal("com.acme.report", err)
}
w := app.Window()
w.SetTitle("报表中心 · 已连接")
w.Maximize()
_ = app.Wait()
```

**开发者不写 app.json**：`Config` 会被落成一份临时 app.json（`%TEMP%\gobrowser-<appId>.app.json`）。
需要完全掌控时用 `gobrowser.NewFromFile("Report.app.json")` 走底层模式（见 §3）。

> 多个产品放进同一个目录时，配置文件必须与 exe 同名（`Report.app.json`）；SDK 查找顺序是 `<程序名>.app.json` → `app.json`。

> ⚠️ 窗口属于外层 `gobrowser.exe` 进程，不属于你的 Go 进程 —— 用任务管理器/EnumWindows 按 Go 进程找窗口是找不到的。

---

## 1. 目录结构

```
gobrowser/
├── app/                     ← 底座 JS（被打成 resources/app.asar）
│   ├── main.js              参数 / 控制面 HTTP / 窗口 / 托盘 / 对话框 / 通知 / IPC
│   ├── preload.js           注入 window.webshell
│   └── package.json
│   ├── facade.go            类库门面：Config / New / Show / ShowDir / ShowFS / ShowURL
│   ├── shell.go             底层：Start / 握手 / 事件 / HTTP 客户端
│   ├── runtime.go           只找同级 runtime + 完整性校验（Probe / FindRuntimeAt）
│   ├── window.go            Window：最大化/最小化/标题/位置/截屏
│   ├── system.go            托盘 / 通知 / 文件对话框 / 消息框
│   ├── notice.go            日志路径 / AppendLog / Fatal
│   └── msgbox_{windows,other}.go  无控制台时的原生错误框
├── internal/asar/           标准 asar 写入（两个工具共用）
├── cmd/hello/               三行示例（New + Show）
├── cmd/demo/                完整示例（embed 前端 + 托盘 + 通知 + 控窗口）
├── tools/pack/              组装运行时（拷贝 dist + 改名 + 打 asar + 校验）
├── tools/build/             交叉编译 + 组装发布目录 + zip + Inno Setup 脚本
├── tools/doctor/            一条命令体检
└── go.mod
```

---

## 2. 三步跑起来

```bash
# ① 组装运行时（把官方加固 dist 变成 gobrowser runtime）
go run ./tools/pack -dist <electron dist> -out C:\tmp_ws\runtime -app ./app

# ② 体检
go run ./tools/doctor -dir C:\tmp_ws

# ③ 打包（GUI 子系统，双击不弹控制台）
go run ./tools/build -targets win-x64 -bundle bundled -runtime-dir C:\tmp_ws\runtime -out C:\tmp_ws\dist -iss

# 不想管运行时？加 -fetch，自动下载对应平台的运行时再组装
go run ./tools/build -targets win-x64,linux-arm64 -bundle bundled -fetch -out dist
```

产物：

```
dist\
├── win-x64\
│   ├── demo.exe          ← 5.4 MB，PE Subsystem=2（GUI，无控制台）
│   ├── demo.app.json     ← 与 exe 同名，按程序名自动加载
│   └── runtime\          ← 174 MB（gobrowser.exe + electron.sign + lic + resources\app.asar）
├── demo-win-x64.zip      ← 80.7 MB，sha256 已打印
└── demo.iss              ← 用 Inno Setup 6 编译成安装包
```

跨平台打包（运行时从官方 zip 解出，自动改名 + 塞 asar）：

```bash
go run ./tools/build -targets win-x64,linux-arm64,linux-loong64 -bundle bundled -zips C:\aitools\00.360易创
```

支持目标：`win-x64` `win-x86` `linux-x64` `linux-arm64` `linux-loong64` `linux-mips64el`（**sw64 无 Go 支持**）。

---

## 2.5 发布形态：runtime 目录 还是 单文件

```bash
# A. 目录 + 自带 runtime（客户机器干净）
go run ./tools/build -targets win-x64 -bundle bundled -runtime-dir D://runtime -out dist

# B. 目录 + 复用同级 runtime（机器上已有 runtime，产物只有几百 KB）
go run ./tools/build -targets win-x64 -bundle shared -out dist

# C. 单文件（runtime 压进 exe 尾部，首次运行自动释放）
go run ./tools/build -targets win-x64 -single -zips C://aitools//00.360易创 -out dist
```

| 形态 | 产物 | 首次启动 | 升级 runtime | 适用 |
|---|---|---|---|---|
| A 目录自带 | `demo.exe 5.4 MB` + `runtime\ 174 MB`（zip 80.7 MB） | 1.5 s | 换目录 | 常规交付 |
| B 复用 | `demo.exe 5.4 MB` | 1.5 s | 换同级目录 | 机器上已有产品 |
| C 单文件 | `hello.exe 83.9 MB`（内含 78.4 MB 载荷） | **4.6 s（多 2.1 s 解压）**，之后 2.5 s | 重打包 exe | U 盘/绿色单文件 |

### zip 是怎么按平台挑的

`-targets` 里的每个目标固定映射到一个官方包，`-zips` 目录里找同名 zip：

| target | GOOS/GOARCH | 官方 zip | runtime 里的二进制 |
|---|---|---|---|
| `win-x64` | windows/amd64 | `electron-v16.0.9-win32-x64.zip` | `gobrowser.exe` |
| `win-x86` | windows/386 | `electron-v16.0.9-win32-ia32.zip` | `gobrowser.exe` |
| `linux-x64` | linux/amd64 | `electron-v16.0.9-linux-x64.zip` | `gobrowser` |
| `linux-arm64` | linux/arm64 | `electron-v16.0.9-linux-arm64.zip` | `gobrowser` |
| `linux-loong64` | linux/loong64 | `electron-v16.0.9-linux-loong64.zip` | `gobrowser` |
| `linux-mips64el` | linux/mips64le | `electron-v16.0.9-linux-mips64el.zip` | `gobrowser` |

解压后自动做两件事：`electron(.exe)` → `gobrowser(.exe)`，把底座 `app.asar` 注入 `resources\`。
工具也支持 `-runtime-dir`（本机平台直接复制现成 runtime，不必再解压）。

### 运行期从哪拿 runtime（按顺序）

1. **同级 `runtime\`**（`..\runtime` 然后 `.\runtime`，`preferBundled` 可调换）
2. **exe 内载荷**：解压到 `%LOCALAPPDATA%\gobrowser\<appId>\runtime-<sha12>\`（Linux: `~/.cache/gobrowser/...`）
3. 都没有 → 报错（GUI 程序会弹原生框 + 写日志）

载荷格式（`internal/payloadfmt`）：

```
[ exe 原始内容 ][ runtime.zip ][ trailer 36B ]
trailer = magic "WSPAYLD1"(8) | version(4) | offset(8) | size(8) | sha256前8字节(8)
```

- PE / ELF 都允许在文件末尾追加数据，原 exe 照常执行（实测：追加 78.4 MB 后窗口正常）。
- 解压目录里有 `.payload` 标记（完整 sha），**摘要变了就重新解压** —— 换 exe 等于自动换 runtime。
- 缓存目录可以随时删，下次启动重建；`shell.Payload` 的 `Extract/PrepareRuntime` 也可单独调用。
- 代价：exe 变大 84 MB、首次启动多 2 秒、部分杀软对"自解压大 exe"更敏感。

## 3. 实测结果（2026-10-21，Win10 VM）

```
[boot]        exe=C:\tmp_ws\ReportApp.exe  app=…\ReportApp.app.json
[runtime]     dir=…\runtime  version=16.0.9  electron=16.0.9  browser360=10.6.1001.11
[ui]          port=3883
[event]       app.ready / window.loaded(http://127.0.0.1:3883/) / window.focus
[info-before] title=报表中心  bounds=1000x680  isMaximized=false  tray=false
[tray]        已创建
[notify]      已发送
[event]       window.maximized
[maximize]    isMaximized=true
[info-after]  title=报表中心 · 已连接  bounds=1936x1056  isMaximized=true  tray=true  bridgeCalls=1
[done]        <nil>
```

| 能力 | 状态 |
|---|---|
| 找同级 runtime（`..\runtime` → `.\runtime`） | ✅ 缺失时日志给出完整路径与缺项 |
| stdout 握手（跳空行） | ✅ 端口 + token |
| 无边框窗口 + 页面自定义标题栏 | ✅ |
| HTTP 控窗口（标题/最大化/最小化/关闭/位置） | ✅ |
| 页面 `window.webshell` 桥 | ✅ `bridgeCalls=1` |
| 事件回流（ready/loaded/focus/maximized/closed） | ✅ |
| 托盘（图标 + 菜单 + 点击显隐） | ✅ `tray=true` |
| 系统通知 | ✅ |
| 文件对话框 / 消息框 | ✅ 接口就绪（`/v1/dialog/*`） |
| **无控制台** | ✅ PE Subsystem=2 |
| doctor 体检 | ✅ 10 项全过 |
| 多平台打包 | ✅ win-x64 实跑；linux-arm64 产物结构核对通过 |
| 安装包 | ⚠️ 生成 `.iss`，需装 Inno Setup 编译 |

---

## 4. 踩过的坑（都修了 / 都写进注释了）

| 现象 | 原因 | 处理 |
|---|---|---|
| 双击先弹黑窗 | Go 默认 console 子系统 | `-ldflags "-H windowsgui"`（build 工具默认加） |
| 无控制台后错误看不见 | GUI 程序没有 stdout | `gobrowser.Fatal()`：写 `%LOCALAPPDATA%\<appId>\logs\shell.log` + 原生 MessageBoxW；`GOBROWSER_SILENT=1` 可跳过弹框（CI 用） |
| `Lock file can not be created! Error code: 3` | 单实例锁要在 userData 里建锁文件，而 userData 目录还不存在 | 先 `mkdirSync` 再 `setPath('userData', …)` |
| `sandbox:true` 页面加载失败 `ERR_FAILED(-2)` | 本运行时下 sandbox + 渲染进程起不来 | 固定 `sandbox:false` + `contextIsolation:true` |
| `capturePage()` 返回 0×0 | 本 VM 的合成限制（重试/隐藏后截都无效） | 接口保留并返回可读错误，不当作验证手段 |
| 握手读到空行 | Electron 启动会先写一个 `\r\n` | SDK 跳过空行，只认以 `{` 开头的行 |

---

## 5. `window.webshell` 暴露的能力

```js
window.webshell.window.minimize() / maximize() / close() / toggleFullScreen()
window.webshell.window.setTitle('x') / isMaximized() / reload()
window.webshell.app.ping() / quit() / platform
window.webshell.on(event, handler)
```

页面侧只需要一行 CSS 就能拖动窗口：`style="-webkit-app-region: drag"`（按钮上加 `no-drag`）。

---

## 6. 事件通道：只留最小集（不做 WS）

当前业务没有消费事件的场景，所以不投入 WS。保留的事件只有低频的几种：

`window.closed / maximized / restored / minimized`、`tray.click`、`menu.click`、`second-instance`、`app.ready`

`focus` / `blur` / `resize` / `moved` 已删除（高频、无消费方、只刷日志）。

托盘菜单另有内置动作，不需要 Go 参与：

```json
"tray": { "menu": [
  { "id": "show", "label": "显示", "action": "show" },
  { "id": "hide", "label": "隐藏", "action": "hide" },
  { "id": "site", "label": "官网", "action": "open:https://example.com" },
  { "id": "quit", "label": "退出", "action": "quit" }
]}
```

## 7. 还没做

- 多显示器定位 / 托盘气泡 / 开机自启
- Linux 实机回归（托盘依赖 libappindicator；`chrome-sandbox` 需要 4755）
- go-winres 图标（本机未安装，build 会提示而不是失败）

---

<sub>改模块路径、发版、运行时打包这些维护动作见 [MAINTAINING.md](MAINTAINING.md)。</sub>
