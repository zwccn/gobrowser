// Package runtimefetch 负责"把运行时弄到本地"：按平台从发布地址下载、
// 校验 sha256、解压到用户缓存目录；也支持从本地目录直接取，避免每次都联网。
//
// 它不知道 gobrowser 的上层语义（appId、载荷、启动），只回答一个问题：
// 给定平台，返回一个可以直接当 runtime\ 用的目录。
package runtimefetch

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed runtime-manifest.json
var manifestJSON []byte

// Manifest 是发布清单：每个平台对应一个 zip 及其摘要。
type Manifest struct {
	Version   string           `json:"version"`
	Release   string           `json:"release"`
	Base      string           `json:"base"`
	Platforms map[string]Entry `json:"platforms"`
}

// Entry 描述某个平台的运行时包。
type Entry struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Load 解析内嵌清单。
func Load() (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("解析运行时清单失败: %w", err)
	}
	if len(m.Platforms) == 0 {
		return nil, fmt.Errorf("运行时清单里没有任何平台")
	}
	return &m, nil
}

// Key 把 GOOS/GOARCH 转成清单里的键（如 windows/amd64、linux/mips64le）。
func Key(goos, goarch string) string {
	return goos + "/" + goarch
}

// Current 返回本机平台的清单键。
func Current() string { return Key(runtime.GOOS, runtime.GOARCH) }

// Entry 取指定平台的条目；base 为空时用清单里的默认地址。
func (m *Manifest) Entry(goos, goarch, baseOverride string) (Entry, string, error) {
	key := Key(goos, goarch)
	e, ok := m.Platforms[key]
	if !ok {
		return Entry{}, "", fmt.Errorf("运行时清单里没有 %s（已知：%s）", key, strings.Join(m.Keys(), ", "))
	}
	base := m.Base
	if baseOverride != "" {
		base = baseOverride
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return e, base + e.File, nil
}

// Keys 列出清单支持的平台（排序，便于报错时展示）。
func (m *Manifest) Keys() []string {
	keys := make([]string, 0, len(m.Platforms))
	for k := range m.Platforms {
		keys = append(keys, k)
	}
	// 简单插入排序，数量很小
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// CacheRoot 是运行时的缓存根目录：%LOCALAPPDATA%\gobrowser\runtimes（Linux 用用户缓存目录）。
func CacheRoot() string {
	if v := os.Getenv("GOBROWSER_RUNTIME_CACHE"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "gobrowser", "runtimes")
		}
	}
	if v, err := os.UserCacheDir(); err == nil {
		return filepath.Join(v, "gobrowser", "runtimes")
	}
	return filepath.Join(os.TempDir(), "gobrowser", "runtimes")
}
