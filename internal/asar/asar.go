// Package asar 写标准 asar 归档。
//
// 布局：
//
//	[u32 4][u32 headerLen] + [u32 payloadLen][u32 jsonLen][json][pad] + 文件内容
//	payloadLen = 4 + jsonLen + pad， headerLen = 4 + payloadLen
package asar

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type node struct {
	Size   int64            `json:"size,omitempty"`
	Offset string           `json:"offset,omitempty"`
	Files  map[string]*node `json:"files,omitempty"`
}

type entry struct {
	rel  string
	abs  string
	size int64
}

// Write 把 srcDir 目录打成 asar 文件。
func Write(srcDir, outPath string) error {
	files, err := collect(srcDir)
	if err != nil {
		return err
	}
	root := &node{Files: map[string]*node{}}
	var offset int64
	for _, f := range files {
		parts := strings.Split(f.rel, "/")
		cur := root
		for _, p := range parts[:len(parts)-1] {
			if cur.Files[p] == nil {
				cur.Files[p] = &node{Files: map[string]*node{}}
			}
			cur = cur.Files[p]
		}
		cur.Files[parts[len(parts)-1]] = &node{Size: f.size, Offset: fmt.Sprint(offset)}
		offset += f.size
	}

	js, err := json.Marshal(root)
	if err != nil {
		return err
	}
	pad := (4 - len(js)%4) % 4
	payload := make([]byte, 4, 4+len(js)+pad)
	binary.LittleEndian.PutUint32(payload, uint32(len(js)))
	payload = append(payload, js...)
	payload = append(payload, make([]byte, pad)...)

	header := make([]byte, 4, 4+len(payload))
	binary.LittleEndian.PutUint32(header, uint32(len(payload)))
	header = append(header, payload...)

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	prefix := make([]byte, 8)
	binary.LittleEndian.PutUint32(prefix[0:], 4)
	binary.LittleEndian.PutUint32(prefix[4:], uint32(len(header)))
	if _, err := out.Write(prefix); err != nil {
		return err
	}
	if _, err := out.Write(header); err != nil {
		return err
	}
	for _, f := range files {
		in, err := os.Open(f.abs)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func collect(root string) ([]entry, error) {
	var files []entry
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, entry{filepath.ToSlash(rel), p, info.Size()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, err
}
