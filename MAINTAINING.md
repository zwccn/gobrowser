# 维护者备忘

面向仓库维护者。普通使用者只看 [README](README.md) 就够。

## 改模块路径（fork / 迁到 Gitee）

一条命令改完 `go.mod` 与所有 import：

```bash
go run ./tools/setmodule -m github.com/你的账号/gobrowser
# 或 gitee.com/你的账号/gobrowser
```

它只改"整行看起来是 import 规格"的行，不会碰 `"gobrowser"` 这类字符串常量（运行时二进制名）。

## 发版

Go 的"库中心"就是模块代理，**不需要注册、不需要上传**，只要仓库公开 + 打 `v*` tag：

```bash
git add -A && git commit -m "gobrowser v0.1.0"
git push origin main
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

推完 tag 之后：

| 生效位置 | 说明 |
|---|---|
| `go get github.com/zwccn/gobrowser@v0.1.0` | 几分钟内可用（代理自动抓取） |
| https://pkg.go.dev/github.com/zwccn/gobrowser | 文档页自动生成（新模块约 30 分钟~数小时） |
| 国内加速 | `GOPROXY=https://goproxy.cn,direct` |

要点：

- 仓库必须公开；私有仓库要 `GOPRIVATE=你的域名` 走直连
- tag 必须是 `v0.1.0` 格式
- `go.mod` 的 module 路径必须与仓库地址一致
- 想立刻确认模块可用：`curl https://goproxy.io/github.com/zwccn/gobrowser/@v/list`

## 关联的 CI

`.github/workflows/ci.yml`：`main` 与 `v*` tag 触发。

- `test`：Windows / macOS / Linux 三平台跑 `gofmt` / `vet` / `test`
- `cross`：8 个目标交叉编译（windows amd64/386、linux amd64/arm64/loong64/mips64le、darwin amd64/arm64）

## 运行时（约 180 MB，不进 Git）

`go get` 只拿 SDK。运行时按平台单独分发，放在本仓库 Releases：

```
gobrowser-runtime-v16.0.9-win32-x64.zip
gobrowser-runtime-v16.0.9-win32-ia32.zip
gobrowser-runtime-v16.0.9-linux-x64.zip
gobrowser-runtime-v16.0.9-linux-arm64.zip
gobrowser-runtime-v16.0.9-linux-loong64.zip
gobrowser-runtime-v16.0.9-linux-mips64el.zip
```

### 一键生成 6 个平台

`tools/relruntime` 直接吃官方平台 zip，产出「解压即可当 `runtime\`」的发布件：
把 `electron(.exe)` 改名为 `gobrowser(.exe)`、注入 19 KB 的 `app.asar`，**并保留原 zip 的权限位**
（Linux 的 `gobrowser` / `chrome-sandbox` 是 `775`，丢了可执行位就跑不起来）。

```bash
go run ./tools/relruntime \
  -zips C:\aitools\00.360易创 \
  -app ./app \
  -out dist/release \
  -version v16.0.9 \
  -platforms win32-x64,win32-ia32,linux-x64,linux-arm64,linux-loong64,linux-mips64el
```

加 `-n` 只检查不写文件。单平台约 12 秒。

> 与 `tools/pack` 的分工：`pack` 处理"本机已有的 dist 目录"（用 `runtime.GOOS` 判断 exe 后缀），
> `relruntime` 处理"跨平台官方 zip"（看 zip 里到底有 `electron` 还是 `electron.exe`）。

### 上传到 Release

```bash
# 1) 建 release（tag 不存在会自动在默认分支创建）
curl -X POST -H "Authorization: token $TOKEN" -H "Content-Type: application/json" \
  --data-binary @release.json \
  https://api.github.com/repos/zwccn/gobrowser/releases

# 2) 传资产（注意 --noproxy：uploads.github.com 直连比走代理快 2.5 倍）
curl -X POST --noproxy '*' -H "Authorization: token $TOKEN" \
  -H "Content-Type: application/zip" --data-binary @<file>.zip \
  "https://uploads.github.com/repos/zwccn/gobrowser/releases/<id>/assets?name=<file>.zip"
```

实测：`api.github.com` / `uploads.github.com` 都能直连；`github.com`（网页与 git）必须走 `127.0.0.1:7897`。
上传速度约 620 KB/s，单包 2–3 分钟。断掉会留下 `state: starter` 的残包，需先 `DELETE /releases/assets/<aid>` 再重传。

## 网络：直连还是代理

本机到 GitHub 的连通性**会变**，两种状态都实测过，别写死：

```bash
# 先判断当前能不能直连（200 就是能）
curl -s -o /dev/null -w "%{http_code}\n" --noproxy '*' \
  "https://github.com/zwccn/gobrowser.git/info/refs?service=git-upload-pack"
```

| 状态 | 做法 |
|---|---|
| 直连 200 | 直接 `git push`，最快（1 秒级）。**不要**再配代理 |
| 直连超时 | `git config http.proxy http://127.0.0.1:7897`，推完可 `git config --unset http.proxy` |

注意：

- `api.github.com`、`uploads.github.com` 基本都能直连；**大文件上传别走代理**（实测慢 2.5 倍，且可能卡在 `state: starter`）
- 代理被占满时，`git push` 会报 `schannel: failed to receive handshake` —— 不是凭据问题，等带宽空出来或改直连
- 凭据走 Windows 凭据管理器（PortableGit 系统配置里已有 `credential.helper=manager`），首次授权：

```bash
env -u GIT_TERMINAL_PROMPT http_proxy=http://127.0.0.1:7897 https_proxy=http://127.0.0.1:7897 \
  "C:/Program Files/Git/mingw64/bin/git-credential-manager.exe" github login --browser --username zwccn
```

> 不要手动把 `credential.helper` 设成带空格的绝对路径 —— git 会当成子命令去拼，
> 报 `credential-C:/Program Files/... is not a git command`。
