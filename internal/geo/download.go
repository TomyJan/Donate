package geo

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"github.com/TomyJan/Donate/internal/observability"
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
		return "", failure("download_prepare", "create_directory_"+observability.ErrorReason(err), 0, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lib.url, nil)
	if err != nil {
		return "", failure("download_request", "invalid_url", 0, err)
	}
	// Seed the headers for same-origin absolute redirects. The updater's
	// redirect policy strips credentials whenever the origin changes.
	if user := req.URL.User; user != nil {
		password, _ := user.Password()
		req.SetBasicAuth(user.Username(), password)
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", failure("download_request", observability.ErrorReason(err), 0, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", failure("download_response", "http_status", resp.StatusCode, ErrUnavailable)
	}
	if resp.ContentLength > maxDatabaseBytes {
		return "", failure("download_body", "response_too_large", 0, ErrUnavailable)
	}
	raw, err := os.CreateTemp(filepath.Dir(lib.path), ".ipdb-download-*")
	if err != nil {
		return "", failure("download_prepare", "create_temp_"+observability.ErrorReason(err), 0, err)
	}
	defer os.Remove(raw.Name())
	n, copyErr := io.Copy(raw, io.LimitReader(resp.Body, maxDatabaseBytes+1))
	closeErr := raw.Close()
	if copyErr != nil || closeErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", failure("download_body", observability.ErrorReason(errors.Join(copyErr, closeErr)), resp.StatusCode, errors.Join(copyErr, closeErr))
	}
	if n > maxDatabaseBytes {
		return "", failure("download_body", "response_too_large", 0, ErrUnavailable)
	}
	out, err := os.CreateTemp(filepath.Dir(lib.path), ".ipdb-stage-*")
	if err != nil {
		return "", failure("download_prepare", "create_stage_"+observability.ErrorReason(err), 0, err)
	}
	stage = out.Name()
	if err := out.Close(); err != nil {
		os.Remove(stage)
		return "", failure("download_prepare", "close_stage_"+observability.ErrorReason(err), 0, err)
	}
	stagedPath := stage
	defer func() {
		if err != nil {
			os.Remove(stagedPath)
		}
	}()
	if err = unpackDatabaseContext(ctx, raw.Name(), stage, lib.kind, maxDatabaseBytes); err != nil {
		var detail *sourceFailure
		if errors.As(err, &detail) {
			return "", failure(detail.stage, detail.reason, resp.StatusCode, err)
		}
		return "", failure("download_unpack", "invalid_archive", resp.StatusCode, err)
	}
	if err = validateDatabaseContext(ctx, stage, lib.kind); err != nil {
		return "", failure("database_validate", "invalid_database", resp.StatusCode, err)
	}
	return stage, nil
}

func unpackDatabase(source, target, kind string, limit int64) error {
	return unpackDatabaseContext(context.Background(), source, target, kind, limit)
}

func unpackDatabaseContext(ctx context.Context, source, target, kind string, limit int64) error {
	f, err := os.Open(source)
	if err != nil {
		return failure("database_read", "open_"+observability.ErrorReason(err), 0, err)
	}
	defer f.Close()
	b := bufio.NewReader(f)
	magic, _ := b.Peek(4)
	if len(magic) >= 2 && magic[0] == 'P' && magic[1] == 'K' {
		info, err := f.Stat()
		if err != nil {
			return failure("database_read", "stat_"+observability.ErrorReason(err), 0, err)
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
		return failure("database_write", "open_"+observability.ErrorReason(err), 0, err)
	}
	n, copyErr := io.Copy(f, io.LimitReader(r, limit+1))
	var syncErr error
	if copyErr == nil && n <= limit {
		syncErr = f.Sync()
	}
	closeErr := f.Close()
	if n > limit {
		return errors.New("库解压数据超限")
	}
	if copyErr != nil {
		return failure("database_write", "copy_"+observability.ErrorReason(copyErr), 0, errors.Join(copyErr, closeErr))
	}
	if syncErr != nil {
		return failure("database_write", "sync_"+observability.ErrorReason(syncErr), 0, errors.Join(syncErr, closeErr))
	}
	if closeErr != nil {
		return failure("database_write", "close_"+observability.ErrorReason(closeErr), 0, closeErr)
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
