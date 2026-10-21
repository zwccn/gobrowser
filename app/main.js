'use strict'
/**
 * gobrowser 底座（运行在 Electron 主进程里，被打包成 resources/app.asar）
 *
 * 职责：
 *   1. 读 --app=<app.json>
 *   2. 起本地控制面（127.0.0.1:随机端口 + token）
 *   3. 向 stdout 打一行握手 JSON（Go 侧读这一行）
 *   4. 按 app.json 建窗口、加载后端 URL
 *   5. 处理页面 preload 的 IPC，处理 Go 的 HTTP 指令
 */
const { app, BrowserWindow, ipcMain, shell, Tray, Menu, nativeImage, Notification, dialog } = require('electron')
const path = require('node:path')
const fs = require('node:fs')
const http = require('node:http')
const crypto = require('node:crypto')

// ------------------------------------------------------------------ 参数
const argv = process.argv.slice(1)
const arg = (name, def) => {
  const p = `--${name}=`
  const hit = argv.find(a => a.startsWith(p))
  return hit ? hit.slice(p.length) : def
}

const appJsonPath = arg('app')
if (!appJsonPath) fatal('缺少 --app=<app.json>')

let cfg
try {
  cfg = JSON.parse(fs.readFileSync(appJsonPath, 'utf8'))
} catch (e) {
  fatal('读取 app.json 失败: ' + e.message)
}

const appId = cfg.appId || 'com.gobrowser.app'
const appName = cfg.name || appId
const token = crypto.randomBytes(16).toString('hex')
const eventsUrl = arg('events', '')
const entryURL = arg('entry', (cfg.entry && cfg.entry.url) || '')
const waitCfg = Object.assign({ timeoutMs: 15000, intervalMs: 200 }, (cfg.entry && cfg.entry.waitFor) || {})
const winDefaults = Object.assign(
  { width: 1280, height: 800, frame: true, center: true, icon: 'icon.png', minWidth: 0, minHeight: 0 },
  cfg.window || {}
)

// ------------------------------------------------------------------ 隔离
// 同一个 appId 才有同一份数据；不同产品之间彻底分开
// 注意：目录必须先建好，否则 requestSingleInstanceLock 会报
//       "Lock file can not be created! Error code: 3" 然后直接退出
const userDataDir = path.join(app.getPath('appData'), appId)
fs.mkdirSync(userDataDir, { recursive: true })
app.setPath('userData', userDataDir)
app.setName(appName)
if (cfg.gpu === 'disable') app.disableHardwareAcceleration()
// gpu: "safe" 用于 VM / 远程桌面 / 无核显的服务器机器：只关硬件加速不够，
// 窗口会"GPU process exited unexpectedly"后闪退（实测 10s 内 FATAL）。
// 这组开关是逐个试出来的组合，缺一不可。
if (cfg.gpu === 'safe') {
  for (const s of ['disable-gpu', 'disable-gpu-compositing', 'disable-software-rasterizer', 'disable-dev-shm-usage', 'no-sandbox']) {
    app.commandLine.appendSwitch(s)
  }
  app.disableHardwareAcceleration()
}
// 自定义开关兜底：特殊情况可自行追加 Chromium 命令行参数
for (const s of (cfg.extraSwitches || [])) {
  if (s) app.commandLine.appendSwitch(String(s))
}
if (cfg.allowMultiple !== true) app.requestSingleInstanceLock() || app.exit(0)

// ------------------------------------------------------------------ 状态
const wins = new Map()      // id -> BrowserWindow
const meta = new Map()      // id -> { url, loaded }
let bridgeCalls = 0
let controlPort = 0
let tray = null

// 默认图标（32×32 PNG）：app.json 没给 icon、或给的路径不存在时用它，保证托盘一定能显示
const DEFAULT_ICON = 'iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAAYUlEQVR42u3WMQoAIAiFYU/Sebto0NwFam0JTAWDfsEx3keLT0Q5fcx5sxI9AACkAW6DQyHecBciKtyM2B+X2kyrAmj0EYDjzwAAAAAAgHRA+jF69hynF5InKhmtGMAXgAWalicwOY2hvgAAAABJRU5ErkJggg=='

function resolvePath (p) {
  return path.isAbsolute(p) ? p : path.join(path.dirname(appJsonPath), p)
}

function loadIcon (rel) {
  if (rel) {
    const p = resolvePath(rel)
    try {
      if (fs.existsSync(p)) {
        const img = nativeImage.createFromPath(p)
        if (!img.isEmpty()) return img
      }
    } catch (_) {}
  }
  return nativeImage.createFromBuffer(Buffer.from(DEFAULT_ICON, 'base64'))
}

function fatal (msg) {
  try { process.stderr.write('[gobrowser] ' + msg + '\n') } catch (_) {}
  process.exit(2)
}

function log (msg) {
  if (cfg.verbose) process.stderr.write('[gobrowser] ' + msg + '\n')
}

// ------------------------------------------------------------------ 事件上报（外壳 -> Go）
function emit (event, payload) {
  if (!eventsUrl) return
  const body = JSON.stringify({ event, appId, ts: Date.now(), payload: payload || {} })
  try {
    const u = new URL(eventsUrl)
    const req = http.request({
      hostname: u.hostname, port: u.port, path: u.pathname, method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) }
    }, res => res.resume())
    req.on('error', () => {})
    req.end(body)
  } catch (_) {}
}

// ------------------------------------------------------------------ 控制面
function send (res, code, obj) {
  const s = JSON.stringify(obj)
  res.writeHead(code, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': Buffer.byteLength(s) })
  res.end(s)
}
const ok = (res, data) => send(res, 200, { ok: true, data: data === undefined ? null : data })
const bad = (res, code, message) => send(res, code, { ok: false, error: { code, message } })

function readBody (req) {
  return new Promise(resolve => {
    let b = ''
    req.on('data', c => (b += c))
    req.on('end', () => { try { resolve(b ? JSON.parse(b) : {}) } catch (_) { resolve({}) } })
  })
}

function info (id) {
  const w = wins.get(id)
  if (!w || w.isDestroyed()) return null
  const m = meta.get(id) || {}
  return {
    id,
    title: w.getTitle(),
    url: m.url,
    loaded: !!m.loaded,
    bounds: w.getBounds(),
    isMaximized: w.isMaximized(),
    isMinimized: w.isMinimized(),
    isFullScreen: w.isFullScreen(),
    isVisible: w.isVisible()
  }
}

async function createWindow (opts) {
  const id = opts.id || 'main'
  const wcfg = Object.assign({}, winDefaults, opts.window || {})
  const url = opts.url || entryURL
  const win = new BrowserWindow({
    width: wcfg.width,
    height: wcfg.height,
    minWidth: wcfg.minWidth,
    minHeight: wcfg.minHeight,
    frame: wcfg.frame !== false,
    center: wcfg.center !== false,
    show: false,
    backgroundColor: wcfg.backgroundColor || '#ffffff',
    title: wcfg.title || appName,
    icon: wcfg.icon ? resolvePath(wcfg.icon) : undefined,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false, // 实测：sandbox:true 在本运行时下页面加载失败
      spellcheck: false
    }
  })

  wins.set(id, win)
  meta.set(id, { url, loaded: false })

  win.webContents.setWindowOpenHandler(({ url: u }) => {
    shell.openExternal(u)
    return { action: 'deny' }
  })

  win.webContents.on('did-finish-load', () => {
    const m = meta.get(id); if (m) m.loaded = true
    emit('window.loaded', { windowId: id, url })
  })
  win.on('closed', () => { wins.delete(id); meta.delete(id); emit('window.closed', { windowId: id }) })
  if (cfg.closeToTray) {
    // 点关闭不退出，收进托盘；真正退出走托盘菜单或 /v1/shutdown
    win.on('close', e => {
      if (!app.__quitting) { e.preventDefault(); win.hide() }
    })
  }
  // 事件只发"业务可能真的要处理"的几种；focus/blur/resize 这类高频事件不发，
  // 避免把事件通道变成日志噪声（真需要时再单独加开关）。
  win.on('maximize', () => emit('window.maximized', { windowId: id }))
  win.on('unmaximize', () => emit('window.restored', { windowId: id }))
  win.on('minimize', () => emit('window.minimized', { windowId: id }))

  try {
    await waitForURL(url, waitCfg)
    await win.loadURL(url)
  } catch (e) {
    log('加载失败: ' + e.message)
    await win.loadURL('data:text/html;charset=utf-8,' + encodeURIComponent(errorPage(url, e.message)))
  }
  if (wcfg.show !== false) win.show()
  if (wcfg.maximized) win.maximize()
  return info(id)
}

function errorPage (url, msg) {
  return `<!doctype html><meta charset="utf-8"><title>无法连接</title>
<style>body{font:14px/1.7 "Microsoft YaHei",system-ui;margin:0;height:100vh;display:flex;
align-items:center;justify-content:center;background:#1b1f24;color:#dbe3ec}
.b{max-width:560px;padding:28px 32px;border:1px solid #333b45;border-radius:12px;background:#232930}
code{color:#7fd1ff;word-break:break-all}</style>
<div class="b"><h2 style="margin:0 0 12px">无法连接到应用后端</h2>
<p>地址：<code>${url}</code></p><p>原因：<code>${String(msg).replace(/</g, '&lt;')}</code></p>
<p>请确认 Go 程序已启动并且该地址可访问。</p></div>`
}

function waitForURL (url, { timeoutMs, intervalMs }) {
  const deadline = Date.now() + timeoutMs
  return new Promise((resolve, reject) => {
    const tick = () => {
      const u = new URL(url)
      const req = http.request(
        { hostname: u.hostname, port: u.port, path: u.pathname + (u.search || ''), method: 'GET', timeout: 1500 },
        res => { res.resume(); resolve(true) }
      )
      req.on('error', retry)
      req.on('timeout', () => { req.destroy(); retry() })
      req.end()
      function retry () {
        if (Date.now() > deadline) return reject(new Error(`等待 ${url} 超时（${timeoutMs}ms）`))
        setTimeout(tick, intervalMs)
      }
    }
    tick()
  })
}

// ------------------------------------------------------------------ 托盘
function buildMenu (items) {
  return Menu.buildFromTemplate((items || []).map(it => {
    if (!it || it.type === 'separator') return { type: 'separator' }
    return {
      id: it.id,
      label: it.label,
      type: it.type || 'normal',
      checked: !!it.checked,
      enabled: it.enabled !== false,
      click: () => {
        emit('menu.click', { id: it.id, label: it.label })
        // 内置动作：不需要 Go 参与也能干活（避免为了一个菜单项就跑事件通道）
        const act = it.action || ''
        if (act === 'quit' || it.id === '__quit') app.quit()
        else if (act === 'show') { const w = mainWindow(); if (w) { w.show(); w.focus() } }
        else if (act === 'hide') { const w = mainWindow(); if (w) w.hide() }
        else if (act.startsWith('open:')) shell.openExternal(act.slice(5))
      }
    }
  }))
}

function mainWindow () {
  for (const w of wins.values()) if (!w.isDestroyed()) return w
  return null
}

function setTray (opts) {
  const t = Object.assign({ icon: winDefaults.icon, tooltip: appName, menu: [] }, cfg.tray || {}, opts || {})
  if (!tray) {
    tray = new Tray(loadIcon(t.icon))
    tray.on('click', () => {
      const w = mainWindow()
      if (w) {
        if (w.isVisible() && w.isFocused()) w.hide()
        else { w.show(); w.focus() }
      }
      emit('tray.click', {})
    })
    tray.on('double-click', () => { const w = mainWindow(); if (w) { w.show(); w.focus() } })
  }
  tray.setToolTip(t.tooltip || appName)
  const img = loadIcon(t.icon)
  if (!img.isEmpty()) tray.setImage(img)
  tray.setContextMenu(buildMenu(t.menu))
  return true
}

// ------------------------------------------------------------------ 路由
async function handle (req, res) {
  if (req.headers.authorization !== 'Bearer ' + token) return bad(res, 401, 'invalid token')
  const u = new URL(req.url, 'http://127.0.0.1')
  const p = u.pathname
  const seg = p.split('/').filter(Boolean)

  if (p === '/v1/health') return ok(res, { status: 'ok' })
  if (p === '/v1/info') {
    return ok(res, {
      appId, name: appName, version: app.getVersion(), pid: process.pid,
      platform: process.platform, arch: process.arch,
      versions: { electron: process.versions.electron, chrome: process.versions.chrome, browser360: process.versions.browser360 || null },
      bridgeCalls, tray: !!tray, windows: Array.from(wins.keys()).map(info).filter(Boolean)
    })
  }
  if (p === '/v1/shutdown') { ok(res); setTimeout(() => app.quit(), 50); return }

  // ---- 托盘
  if (p === '/v1/tray' && req.method === 'POST') {
    try { setTray(await readBody(req)); return ok(res, { tray: true }) } catch (e) { return bad(res, 500, '托盘创建失败: ' + e.message) }
  }
  if (p === '/v1/tray/destroy' && req.method === 'POST') {
    if (tray) { tray.destroy(); tray = null }
    return ok(res, { tray: false })
  }
  if (p === '/v1/tray/menu' && req.method === 'POST') {
    if (!tray) return bad(res, 400, '托盘还没创建')
    const b = await readBody(req)
    tray.setContextMenu(buildMenu(b.menu || []))
    return ok(res)
  }

  // ---- 系统对话框
  if (p === '/v1/dialog/open' && req.method === 'POST') {
    const b = await readBody(req)
    const r = await dialog.showOpenDialog(mainWindow(), {
      title: b.title, defaultPath: b.defaultPath, filters: b.filters,
      properties: b.properties || ['openFile', 'multiSelections']
    })
    return ok(res, { canceled: r.canceled, paths: r.filePaths })
  }
  if (p === '/v1/dialog/save' && req.method === 'POST') {
    const b = await readBody(req)
    const r = await dialog.showSaveDialog(mainWindow(), {
      title: b.title, defaultPath: b.defaultPath, filters: b.filters
    })
    return ok(res, { canceled: r.canceled, path: r.filePath })
  }
  if (p === '/v1/dialog/message' && req.method === 'POST') {
    const b = await readBody(req)
    const r = await dialog.showMessageBox(mainWindow(), {
      type: b.type || 'info', title: b.title || appName, message: b.message || '',
      detail: b.detail, buttons: b.buttons || ['确定'], defaultId: b.defaultId || 0,
      cancelId: b.cancelId
    })
    return ok(res, { response: r.response, checkboxChecked: r.checkboxChecked })
  }

  // ---- 系统通知
  if (p === '/v1/notify' && req.method === 'POST') {
    const b = await readBody(req)
    if (!Notification.isSupported()) return bad(res, 501, '当前系统不支持通知')
    const n = new Notification({
      title: b.title || appName, body: b.body || '',
      icon: b.icon ? loadIcon(b.icon) : loadIcon(winDefaults.icon),
      silent: !!b.silent
    })
    n.on('click', () => emit('notify.click', {}))
    n.show()
    return ok(res)
  }

  if (p === '/v1/windows' && req.method === 'POST') {
    const b = await readBody(req)
    try { return ok(res, await createWindow(b)) } catch (e) { return bad(res, 500, e.message) }
  }

  if (seg[0] === 'v1' && seg[1] === 'windows' && seg[2]) {
    const id = decodeURIComponent(seg[2])
    const w = wins.get(id)
    if (!w || w.isDestroyed()) return bad(res, 404, 'window not found: ' + id)
    const action = seg[3]

    if (!action && req.method === 'GET') return ok(res, info(id))
    if (action === 'close') { w.close(); return ok(res) }
    if (action === 'maximize') { w.maximize(); return ok(res) }
    if (action === 'unmaximize') { w.unmaximize(); return ok(res) }
    if (action === 'minimize') { w.minimize(); return ok(res) }
    if (action === 'restore') { w.restore(); return ok(res) }
    if (action === 'show') { w.show(); return ok(res) }
    if (action === 'hide') { w.hide(); return ok(res) }
    if (action === 'focus') { w.focus(); return ok(res) }
    if (action === 'fullscreen') { w.setFullScreen(!!(await readBody(req)).on); return ok(res) }
    if (action === 'always-on-top') { w.setAlwaysOnTop(!!(await readBody(req)).on); return ok(res) }
    if (action === 'title' && req.method === 'POST') { w.setTitle((await readBody(req)).title || ''); return ok(res) }
    if (action === 'bounds' && req.method === 'PUT') {
      const b = await readBody(req); w.setBounds(b); return ok(res)
    }
    if (action === 'navigate' && req.method === 'POST') {
      const b = await readBody(req); meta.get(id).url = b.url; await w.loadURL(b.url); return ok(res)
    }
    if (action === 'capture' && req.method === 'POST') {
      // 某些环境（虚拟机/远程桌面/窗口被遮挡）对可见窗口 capturePage 会返回空图；
      // 隐藏后再截往往能拿到最后一帧，所以做一次降级重试。
      let img = null
      for (let i = 0; i < 2; i++) {
        img = await w.webContents.capturePage()
        if (img && img.toPNG().length > 0) break
        await new Promise(r => setTimeout(r, 250))
      }
      if (!img || img.toPNG().length === 0) {
        const wasVisible = w.isVisible()
        w.hide()
        await new Promise(r => setTimeout(r, 250))
        img = await w.webContents.capturePage()
        if (wasVisible) w.show()
      }
      const png = img ? img.toPNG() : Buffer.alloc(0)
      return ok(res, { size: img ? img.getSize() : null, bytes: png.length, pngBase64: png.toString('base64') })
    }
    return bad(res, 400, 'unknown action: ' + action)
  }
  bad(res, 404, 'not found: ' + p)
}

function startControl () {
  return new Promise(resolve => {
    const srv = http.createServer((req, res) => {
      handle(req, res).catch(e => bad(res, 500, e.message || String(e)))
    })
    srv.listen(0, '127.0.0.1', () => {
      controlPort = srv.address().port
      resolve(controlPort)
    })
  })
}

// ------------------------------------------------------------------ 页面 IPC
ipcMain.handle('ws:ping', e => {
  bridgeCalls++
  return { pong: true, appId, version: app.getVersion() }
})

ipcMain.handle('ws:action', (e, action, payload) => {
  bridgeCalls++
  const w = BrowserWindow.fromWebContents(e.sender)
  if (!w || w.isDestroyed()) return { ok: false }
  switch (action) {
    case 'minimize': w.minimize(); break
    case 'maximize': w.isMaximized() ? w.unmaximize() : w.maximize(); break
    case 'close': w.close(); break
    case 'toggleFullScreen': w.setFullScreen(!w.isFullScreen()); break
    case 'setTitle': w.setTitle(String(payload || '')); break
    case 'isMaximized': return { ok: true, value: w.isMaximized() }
    case 'reload': w.reload(); break
    default: return { ok: false, error: 'unknown action ' + action }
  }
  return { ok: true }
})

ipcMain.handle('ws:quit', () => { app.quit(); return { ok: true } })

// ------------------------------------------------------------------ 启动
app.on('second-instance', () => {
  const first = Array.from(wins.values())[0]
  if (first && !first.isDestroyed()) { if (first.isMinimized()) first.restore(); first.focus() }
  emit('second-instance', {})
})

app.on('window-all-closed', () => app.quit())

app.on('before-quit', () => { app.__quitting = true })

app.on('will-quit', () => { if (tray) { try { tray.destroy() } catch (_) {} tray = null } })

app.whenReady().then(async () => {
  await startControl()

  // 握手：Go 侧读这一行（注意 Electron 启动会先输出一个空行，Go 要跳过）
  process.stdout.write(JSON.stringify({
    event: 'ready', appId, name: appName, port: controlPort, token, pid: process.pid,
    electron: process.versions.electron, browser360: process.versions.browser360 || null
  }) + '\n')

  emit('app.ready', { port: controlPort })

  try {
    await createWindow({ id: 'main' })
    if (cfg.tray && cfg.tray.enabled !== false) {
      try { setTray(cfg.tray) } catch (e) { log('托盘创建失败: ' + e.message) }
    }
  } catch (e) {
    log('创建主窗口失败: ' + e.message)
    app.quit()
  }
})
