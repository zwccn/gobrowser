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

`go get` 只拿 SDK。运行时按平台单独分发，建议放本仓库 Releases：

```
gobrowser-runtime-v16.0.9-win32-x64.zip
gobrowser-runtime-v16.0.9-win32-ia32.zip
gobrowser-runtime-v16.0.9-linux-x64.zip
gobrowser-runtime-v16.0.9-linux-arm64.zip
gobrowser-runtime-v16.0.9-linux-loong64.zip
gobrowser-runtime-v16.0.9-linux-mips64el.zip
```

生成方式（本机平台直接复制现成 runtime，跨平台从官方 zip 解包并自动改名 + 注入 asar）：

```bash
go run ./tools/pack -dist <electron dist 目录> -out D:\Apps\runtime -app ./app
```

## 推送走代理

本机 `github.com:443` 直连不通，仓库已配置：

```bash
git config http.proxy http://127.0.0.1:7897
```

凭据走 Windows 凭据管理器（PortableGit 系统配置里已有 `credential.helper=manager`）。
首次授权：

```bash
env -u GIT_TERMINAL_PROMPT http_proxy=http://127.0.0.1:7897 https_proxy=http://127.0.0.1:7897 \
  "C:/Program Files/Git/mingw64/bin/git-credential-manager.exe" github login --browser --username zwccn
```

> 不要手动把 `credential.helper` 设成带空格的绝对路径 —— git 会当成子命令去拼，
> 报 `credential-C:/Program Files/... is not a git command`。
