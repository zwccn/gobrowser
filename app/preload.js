'use strict'
/**
 * 注入到每个页面的桥：window.webshell
 * 页面不需要打包器、不需要 import，直接可用。
 */
const { contextBridge, ipcRenderer } = require('electron')

const action = (name, payload) => ipcRenderer.invoke('ws:action', name, payload)

contextBridge.exposeInMainWorld('webshell', {
  window: {
    minimize: () => action('minimize'),
    maximize: () => action('maximize'),          // 切换：已最大化则还原
    close: () => action('close'),
    toggleFullScreen: () => action('toggleFullScreen'),
    setTitle: t => action('setTitle', t),
    isMaximized: () => action('isMaximized').then(r => r.value),
    reload: () => action('reload')
  },
  app: {
    ping: () => ipcRenderer.invoke('ws:ping'),
    quit: () => ipcRenderer.invoke('ws:quit'),
    platform: process.platform
  },
  on: (event, handler) => {
    // 预留：外壳 -> 页面 的主动推送
    ipcRenderer.on('ws:event:' + event, (_e, payload) => handler(payload))
    return () => ipcRenderer.removeAllListeners('ws:event:' + event)
  }
})
