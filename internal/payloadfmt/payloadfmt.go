// Package payloadfmt 定义"运行时载荷附加在可执行文件尾部"的格式，读写两侧共用。
//
// 布局：[ exe 原有内容 ][ zip 数据 ][ trailer 36 字节 ]
// trailer = magic(8) | version(4) | offset(8) | size(8) | sha256前8字节(8)
package payloadfmt

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	Magic       = "WSPAYLD1"
	Version     = uint32(1)
	TrailerSize = 36
)

// Trailer 描述尾部载荷。
type Trailer struct {
	Offset int64
	Size   int64
	Sum    []byte // 8 字节
}

// Append 把 zip 追加到 exe 末尾，并写 trailer。返回写入后的载荷信息。
func Append(exePath, zipPath string) (Trailer, error) {
	exe, err := os.OpenFile(exePath, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return Trailer{}, err
	}
	defer exe.Close()
	st, err := exe.Stat()
	if err != nil {
		return Trailer{}, err
	}
	offset := st.Size()

	z, err := os.Open(zipPath)
	if err != nil {
		return Trailer{}, err
	}
	defer z.Close()

	h := sha256.New()
	n, err := io.Copy(exe, io.TeeReader(z, h))
	if err != nil {
		return Trailer{}, err
	}
	sum := h.Sum(nil)[:8]

	tr := make([]byte, TrailerSize)
	copy(tr[0:8], Magic)
	binary.LittleEndian.PutUint32(tr[8:12], Version)
	binary.LittleEndian.PutUint64(tr[12:20], uint64(offset))
	binary.LittleEndian.PutUint64(tr[20:28], uint64(n))
	copy(tr[28:36], sum)

	if _, err := exe.Write(tr); err != nil {
		return Trailer{}, err
	}
	return Trailer{Offset: offset, Size: n, Sum: sum}, nil
}

// Read 读取 exe 尾部载荷信息；没有则返回 ok=false。
func Read(exePath string) (Trailer, bool) {
	f, err := os.Open(exePath)
	if err != nil {
		return Trailer{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < TrailerSize {
		return Trailer{}, false
	}
	buf := make([]byte, TrailerSize)
	if _, err := f.ReadAt(buf, st.Size()-TrailerSize); err != nil {
		return Trailer{}, false
	}
	if string(buf[0:8]) != Magic {
		return Trailer{}, false
	}
	if binary.LittleEndian.Uint32(buf[8:12]) != Version {
		return Trailer{}, false
	}
	off := int64(binary.LittleEndian.Uint64(buf[12:20]))
	size := int64(binary.LittleEndian.Uint64(buf[20:28]))
	if off < 0 || size <= 0 || off+size > st.Size()-TrailerSize {
		return Trailer{}, false
	}
	return Trailer{Offset: off, Size: size, Sum: append([]byte(nil), buf[28:36]...)}, true
}

// Describe 给日志用。
func (t Trailer) Describe() string {
	return fmt.Sprintf("payload %d bytes @%#x sha=%x", t.Size, t.Offset, t.Sum)
}
