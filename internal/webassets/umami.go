package webassets

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io/fs"
)

// WithUmami prepares index.html once at startup. URLs and IDs come from validated configuration.
// Other embedded assets, including their hashed names, remain unchanged.
func WithUmami(assets fs.FS, scriptURL, websiteID string) (fs.FS, error) {
	if scriptURL == "" && websiteID == "" {
		return assets, nil
	}
	if scriptURL == "" || websiteID == "" {
		return nil, errors.New("Umami 脚本地址和网站 ID 必须同时配置")
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("准备 Umami 前端页面失败: %w", err)
	}
	info, err := fs.Stat(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("读取前端页面信息失败: %w", err)
	}
	at := bytes.Index(index, []byte("</head>"))
	if at < 0 {
		return nil, errors.New("准备 Umami 前端页面失败: index.html 缺少 head 结束标签")
	}
	script := []byte(fmt.Sprintf(`<script defer src="%s" data-website-id="%s" data-exclude-search="true" data-exclude-hash="true" data-do-not-track="true"></script>`, html.EscapeString(scriptURL), html.EscapeString(websiteID)))
	prepared := make([]byte, 0, len(index)+len(script))
	prepared = append(prepared, index[:at]...)
	prepared = append(prepared, script...)
	prepared = append(prepared, index[at:]...)
	return &umamiFS{FS: assets, index: prepared, info: indexInfo{FileInfo: info, size: int64(len(prepared))}}, nil
}

type umamiFS struct {
	fs.FS
	index []byte
	info  fs.FileInfo
}

func (f *umamiFS) Open(name string) (fs.File, error) {
	if name == "index.html" {
		return &indexFile{Reader: bytes.NewReader(f.index), info: f.info}, nil
	}
	file, err := f.FS.Open(name)
	if err == nil && name == "." {
		if dir, ok := file.(fs.ReadDirFile); ok {
			return &indexDir{ReadDirFile: dir, info: f.info}, nil
		}
	}
	return file, err
}

type indexDir struct {
	fs.ReadDirFile
	info fs.FileInfo
}

func (d *indexDir) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := d.ReadDirFile.ReadDir(n)
	for i, entry := range entries {
		if entry.Name() == "index.html" {
			entries[i] = fs.FileInfoToDirEntry(d.info)
		}
	}
	return entries, err
}

type indexFile struct {
	*bytes.Reader
	info fs.FileInfo
}

func (f *indexFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *indexFile) Close() error               { return nil }

type indexInfo struct {
	fs.FileInfo
	size int64
}

func (i indexInfo) Size() int64 { return i.size }
