package geo

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxDatabaseBytes int64 = 512 * 1024 * 1024
const maxArchiveEntries = 256

func (u *Updater) download(ctx context.Context, lib databaseSource) (stage string, err error) {
	if err := os.MkdirAll(filepath.Dir(lib.path), 0700); err != nil {
		return "", errors.New("无法创建库目录")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lib.url, nil)
	if err != nil {
		return "", errors.New("下载地址无效")
	}
	// Seed the original headers so net/http can retain authentication on
	// same-host absolute redirects and strip it when the host changes.
	if user := req.URL.User; user != nil {
		password, _ := user.Password()
		req.SetBasicAuth(user.Username(), password)
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("库下载请求失败（网络或超时）")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("库下载返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxDatabaseBytes {
		return "", errors.New("库下载超过 512 MiB")
	}
	raw, err := os.CreateTemp(filepath.Dir(lib.path), ".ipdb-download-*")
	if err != nil {
		return "", errors.New("无法创建下载暂存文件")
	}
	defer os.Remove(raw.Name())
	n, copyErr := io.Copy(raw, io.LimitReader(resp.Body, maxDatabaseBytes+1))
	closeErr := raw.Close()
	if copyErr != nil || closeErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("库下载未完成")
	}
	if n > maxDatabaseBytes {
		return "", errors.New("库下载超过 512 MiB")
	}
	out, err := os.CreateTemp(filepath.Dir(lib.path), ".ipdb-stage-*")
	if err != nil {
		return "", errors.New("无法创建库暂存文件")
	}
	stage = out.Name()
	if err := out.Close(); err != nil {
		os.Remove(stage)
		return "", errors.New("无法关闭库暂存文件")
	}
	stagedPath := stage
	defer func() {
		if err != nil {
			os.Remove(stagedPath)
		}
	}()
	if err = unpackDatabaseContext(ctx, raw.Name(), stage, lib.kind, maxDatabaseBytes); err != nil {
		return "", err
	}
	if err = validateDatabaseContext(ctx, stage, lib.kind); err != nil {
		return "", fmt.Errorf("下载库的格式或协议无效: %w", err)
	}
	return stage, nil
}

func unpackDatabase(source, target, kind string, limit int64) error {
	return unpackDatabaseContext(context.Background(), source, target, kind, limit)
}

func unpackDatabaseContext(ctx context.Context, source, target, kind string, limit int64) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	b := bufio.NewReader(f)
	magic, _ := b.Peek(4)
	if len(magic) >= 2 && magic[0] == 'P' && magic[1] == 'K' {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		archive, err := zip.NewReader(f, info.Size())
		if err != nil {
			return errors.New("ZIP 格式无效")
		}
		if len(archive.File) > maxArchiveEntries {
			return errors.New("压缩包文件数量过多")
		}
		var total uint64
		found := false
		for _, entry := range archive.File {
			if entry.UncompressedSize64 > uint64(limit)-total {
				return errors.New("压缩包解压数据超限")
			}
			total += entry.UncompressedSize64
			if !entry.Mode().IsRegular() || !databaseEntry(entry.Name, kind) {
				continue
			}
			if found {
				return errors.New("压缩包包含多个匹配库")
			}
			found = true
			r, err := entry.Open()
			if err != nil {
				return errors.New("压缩包库文件无法读取")
			}
			err = copyDatabase(target, contextReader{ctx, r}, limit)
			closeErr := r.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return errors.New("压缩包校验失败")
			}
		}
		if !found {
			return errors.New("压缩包未包含匹配库")
		}
		return nil
	}
	var reader io.Reader = b
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		g, err := gzip.NewReader(b)
		if err != nil {
			return errors.New("gzip 格式无效")
		}
		defer g.Close()
		reader = g
	}
	limited := &io.LimitedReader{R: contextReader{ctx, reader}, N: limit + 1}
	decoded := bufio.NewReader(limited)
	header, _ := decoded.Peek(512)
	if len(header) == 512 && string(header[257:262]) == "ustar" {
		archive := tar.NewReader(decoded)
		found := false
		for i := 0; ; i++ {
			h, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return errors.New("tar 数据损坏或截断")
			}
			if i >= maxArchiveEntries {
				return errors.New("压缩包文件数量过多")
			}
			if h.Size < 0 || h.Size > limit {
				return errors.New("压缩包解压数据超限")
			}
			if h.Typeflag != tar.TypeReg || !databaseEntry(h.Name, kind) {
				continue
			}
			if found {
				return errors.New("压缩包包含多个匹配库")
			}
			found = true
			if err := copyDatabase(target, archive, limit); err != nil {
				return err
			}
		}
		if _, err := io.Copy(io.Discard, decoded); err != nil {
			return errors.New("压缩包校验失败")
		}
		if limited.N <= 0 {
			return errors.New("压缩包解压数据超限")
		}
		if !found {
			return errors.New("压缩包未包含匹配库")
		}
		return nil
	}
	return copyDatabase(target, decoded, limit)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func copyDatabase(target string, r io.Reader, limit int64) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("无法写入库暂存文件")
	}
	n, copyErr := io.Copy(f, io.LimitReader(r, limit+1))
	if copyErr == nil && n <= limit {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if n > limit {
		return errors.New("库解压数据超限")
	}
	if copyErr != nil || closeErr != nil {
		return errors.New("库数据不完整或写入失败")
	}
	return nil
}

func databaseEntry(name, kind string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(name)))
	if kind == "mmdb" {
		return strings.HasSuffix(base, ".mmdb")
	}
	if !strings.HasSuffix(base, ".xdb") {
		return false
	}
	if kind == "xdb4" && (strings.Contains(base, "_v6") || strings.Contains(base, "ipv6")) {
		return false
	}
	if kind == "xdb6" && (strings.Contains(base, "_v4") || strings.Contains(base, "ipv4")) {
		return false
	}
	return true
}
