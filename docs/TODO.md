# TODO

> 来源：2026-10-21 的"文档 vs 代码"一致性评审（过程报告已删除）。
> 分类：**[文档]** = 文档写错/过时，照抄会踩坑；**[代码]** = 文档承诺了但代码没实现。
> 已完成项见文末。

---

## P0 · 会让使用者当场踩坑

### 文档待订正

- [ ] **[文档]** 开发者指南 §4「SDK 覆盖的能力」整表按 `facade.go` / `window.go` / `system.go` 重写：`Tray().SetIcon()`、`Dialog()`、`Clipboard()`、`Shortcut()`、`Raw()`、`NewWindow(WindowOptions)`、`SetOpacity()` 全部不存在；真实方法是 `SetTray(TrayOptions)`、`OpenFile(FileDialogOptions)`、`SaveFile`、`MessageBox`、`Notify`、`Client().Do`、`NewWindow(map[string]any)`
- [ ] **[文档]** 架构方案 §7 每个 `app.json` 字段标注实现状态：`security.*`（allowNavigation / devTools / fileSignCheck / encryptStorage / injectBridge）、`shortcuts`、`window.titleBarStyle` / `opacity` / `alwaysOnTop`（创建期）均未实现，写了会被静默忽略
- [ ] **[文档]** 架构方案 §9.1 端点表加 ✔/✗：`clipboard` / `shortcut` / `open-external` / `displays` / `cursor` / `opacity` / `resizable` / `center` 不存在（实际只有 11 组路由）
- [ ] **[文档]** 事件清单对齐代码：删 `app.before-quit`（`main.js` 只设标志、不 emit），补 `window.loaded`、`notify.click`
- [ ] **[文档]** 架构方案 §11.2 删掉"校验 `Origin` / `Sec-Fetch-Site`"（代码只校验 `Authorization: Bearer`）
- [ ] **[文档]** 架构方案 §2 把 `version` 标为可选（`Probe()` 只校验 `electron.sign` / `lic/` / `resources/app.asar` / 可执行文件）
- [ ] **[文档]** 架构方案 §3 握手改为"跳过空行、取第一条以 `{` 开头的行"，与附录 A.7 统一（正文现在仍写"stdout 首行"）
- [ ] **[文档]** 架构方案 §13 回写状态：P1 的"托盘/对话框/通知待补"已完成
- [ ] **[文档]** 架构方案 §12 Linux 统一口径："交叉编译产物已核对，实机回归未做（托盘需 libappindicator，chrome-sandbox 需 4755）"
- [ ] **[文档]** README §1 目录树修正缩进（`facade.go` 等被挂在 `app/` 下），补 `payload.go` / `internal/payloadfmt` / `tools/relruntime` / `tools/setmodule`
- [ ] **[文档]** 统一体积口径：运行时"约 175 MB（win-x64 实测 174 MB）"；exe"空壳 1.9 MB / 带 embed 前端 5.4 MB"
- [ ] **[文档]** 统一平台名 `win-x86`（括号注 GOARCH=386），清掉 `win-ia32`
- [ ] **[文档]** 开发者指南 §2 补一句：Windows 图标/版本信息依赖外部 `go-winres`，未安装时只提示不生成（与 README §7 对齐）
- [ ] **[文档]** 三份文档加统一 header：适用 runtime 版本 / SDK 版本 / 最后更新
- [ ] **[文档]** 示例里的 `Show()` 返回值别丢弃，改成 `if err := …; err != nil { gobrowser.Fatal(…) }`

### 代码待实现

- [ ] **[代码]** 父进程存活绑定：Win `Job Object(KILL_ON_JOB_CLOSE)` / Linux `PR_SET_PDEATHSIG`；现在 Go 被强杀会留下 `gobrowser.exe`
- [ ] **[代码]** 控制 API 补 `Origin` / `Sec-Fetch-Site` 校验（或彻底删掉文档里的这条承诺）
- [ ] **[代码]** `app.setAppUserModelId(appId)`：任务栏分组、通知来源名目前做不到
- [ ] **[代码]** `app.json` 的 `security.*` / `shortcuts` / `titleBarStyle` / `opacity` 要么实现，要么启动时对未知字段告警
- [ ] **[代码]** `app.json` JSON Schema + 外壳启动校验（字段拼错现在完全静默）

---

## P1 · 文档维护与完整度

- [ ] **[发布]** CI 出预组装包：tag 时 Actions 先取运行时再 build，上传 `gobrowser-<platform>-vX.zip`（exe + runtime），客户下一个包就能用
- [ ] **[代码]** 运行期 `fetch` 形态（可选）：`app.json` 里配 `runtime.fetch`/URL，客户机器首次启动下载；默认关，需在文档写清内网/离线风险
- [ ] `docs/README.md`：文档地图（谁看哪份、哪份权威、状态图例）
- [ ] `docs/api.md`：Go 方法表 + HTTP 端点表全量对照（带 ✔/🚧/✗）
- [ ] 事件与错误码表（退出码 2、401 invalid token、握手超时、缺 DLL `0xc0000135`）
- [ ] `docs/troubleshooting.md`：把 README §4"踩过的坑"升级成 症状 → 原因 → 处理 → 验证
- [ ] 安全模型一页：威胁边界、token 生命周期、页面能力边界
- [ ] 升级/回滚：替换 `runtime\` 的注意事项（运行中删不掉）+ 单文件缓存清理
- [ ] `CHANGELOG.md`：约定变更记录格式
- [ ] 性能基线表：内存 120–180 MB 目前是估算，标注或实测
- [ ] `tools/doccheck`：正则比对 `main.js` 路由 / Go 导出方法 / `docs/api.md` 表格，挂 CI 防漂移

---

## 建议顺序

1. 「文档待订正」里的 P0 —— 纯文字，不动代码，先把错误说法清掉
2. `schema/app.json` + 启动校验 —— 大约 30 行，拦住字段拼错
3. `tools/doccheck` 上 CI —— 防止再次漂移
4. 「代码待实现」按排期走（Job Object 优先，属于真 bug）
5. P1 文档按发布节奏补

---

## 已完成（2026-10-21）

- [x] 运行时自动获取：`go run .` 缺运行时自动下载到用户缓存（`internal/runtimefetch`，含清单/校验/解压/缓存 + 5 个单测）；生产构建不联网，靠 `debug.ReadBuildInfo` 区分 go run
- [x] `tools/build -fetch`：一条命令下载并组装 `bundled` 包；镜像/离线走 `GOBROWSER_RUNTIME_BASE` / `GOBROWSER_RUNTIME_DIR` / `GOBROWSER_RUNTIME_CACHE`
- [x] `tools/relruntime -manifest`：打完运行时 zip 顺手把清单（size/sha256）写回 `internal/runtimefetch/`，发版流程闭环，避免手工同步漏掉
- [x] `gpu: "safe"`：VM/远程桌面/无核显机器的防闪退模式（`--disable-gpu --disable-gpu-compositing --disable-software-rasterizer --disable-dev-shm-usage --no-sandbox`，实测只关加速不够）；另支持 `extraSwitches` 自定义开关
- [x] 配置文件按程序名命名：`<程序名>.app.json` 优先、`app.json` 兜底 —— 多产品共用一个统一目录不再互相覆盖（新增 `appjson.go` + 单测，`tools/build` / `cmd/demo` / `tools/doctor` 已接入）
- [x] 命名统一 `webshell` → `gobrowser`（代码文案/路径/包注释 + 文档）
- [x] 两份文档入库 `docs/`，README 加文档入口
- [x] 命令订正：`gobrowser runtime/pack/doctor/run`、`shell doctor` → `go run ./tools/*`；`--target/--mode` → `-targets/-bundle`；包名 `shell.*` → `gobrowser.*`；删未实现的 `SHELL_DEV_URL`
- [x] 移除授权合规表述（保留 `lic/` 作为必备文件的技术说明）
- [x] 删除 `360electron-guide/`（结论已沉淀进代码与 README）
