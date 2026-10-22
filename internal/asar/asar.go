// Package asar 写标准 asar 归档。
//
// 布局：
//
//	[u32 4][u32 headerLen] + [u32 payloadLen][u32 jsonLen][json][pad] + 文件内容
//	payloadLen = 4 + jsonLen + pad， headerLen = 4 + payloadLen
package asar

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
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

// srcFile 是打包输入：来源可以是磁盘文件，也可以是内存里的 fs.FS。
type srcFile struct {
	rel  string
	size int64
	open func() (io.ReadCloser, error)
}

// Write 把 srcDir 目录打成 asar 文件。
func Write(srcDir, outPath string) error {
	entries, err := collect(srcDir)
	if err != nil {
		return err
	}
	files := make([]srcFile, 0, len(entries))
	for _, e := range entries {
		abs := e.abs
		files = append(files, srcFile{rel: e.rel, size: e.size, open: func() (io.ReadCloser, error) {
			return os.Open(abs)
		}})
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return pack(files, out)
}

// PackFS 把 fs.FS 的根目录打成 asar 并返回字节。
//
// SDK 用它把内置底座（app/*）直接写进 runtime/resources/app.asar ——
// 这样"改了底座 JS"不必重发几十 MB 的运行时包。
func PackFS(fsys fs.FS) ([]byte, error) {
	var files []srcFile
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel := p
		files = append(files, srcFile{rel: rel, size: info.Size(), open: func() (io.ReadCloser, error) {
			return fsys.Open(rel)
		}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	var buf bytes.Buffer
	if err := pack(files, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pack 把文件列表写成 asar 流。
func pack(files []srcFile, w io.Writer) error {
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

	prefix := make([]byte, 8)
	binary.LittleEndian.PutUint32(prefix[0:], 4)
	binary.LittleEndian.PutUint32(prefix[4:], uint32(len(header)))
	if _, err := w.Write(prefix); err != nil {
		return err
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	for _, f := range files {
		in, err := f.open()
		if err != nil {
			return err
		}
		_, err = io.Copy(w, in)
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
