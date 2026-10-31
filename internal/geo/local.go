package geo

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/oschwald/maxminddb-golang/v2"
)

type xdbReader struct {
	reader  *xdb.Searcher
	mu      sync.Mutex
	version string
	legacy  bool
}

func openXDB(path string, version *xdb.Version) (*xdbReader, error) {
	if err := xdb.VerifyFromFile(path); err != nil {
		return nil, err
	}
	header, err := xdb.LoadHeaderFromFile(path)
	if err != nil {
		return nil, err
	}
	actual, err := xdb.VersionFromHeader(header)
	if err != nil {
		return nil, err
	}
	if actual.Id != version.Id {
		return nil, fmt.Errorf("文件为 %s，配置需要 %s", actual.Name, version.Name)
	}
	if header.IndexPolicy != xdb.VectorIndexPolicy {
		return nil, fmt.Errorf("不支持的 XDB 索引策略 %d", header.IndexPolicy)
	}
	index, err := xdb.LoadVectorIndexFromFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	start, end, size := uint64(header.StartIndexPtr), uint64(header.EndIndexPtr), uint64(version.SegmentIndexSize)
	if start < xdb.HeaderInfoLength+uint64(len(index)) || end < start || (end-start)%size != 0 || end+size > uint64(info.Size()) {
		return nil, fmt.Errorf("XDB 分段索引越界或不完整")
	}
	for i := 0; i < len(index); i += 8 {
		a, b := uint64(binary.LittleEndian.Uint32(index[i:])), uint64(binary.LittleEndian.Uint32(index[i+4:]))
		if a == 0 && b == 0 {
			continue
		}
		// Official files use the exclusive segment boundary in their final
		// vector. The Go reader searches inclusively, so normalize this single
		// sentinel without permitting other out-of-range vector pointers.
		if i == len(index)-8 && b == end+size {
			b = end
			binary.LittleEndian.PutUint32(index[i+4:], uint32(b))
		}
		if a < start || b < a || b > end || (a-start)%size != 0 || (b-start)%size != 0 {
			return nil, fmt.Errorf("XDB 向量索引 %d 越界或不完整", i/8)
		}
	}
	reader, err := xdb.NewWithVectorIndex(version, path, index)
	if err != nil {
		return nil, err
	}
	return &xdbReader{reader: reader, version: time.Unix(int64(header.CreatedAt), 0).UTC().Format(time.RFC3339), legacy: header.Version == xdb.Structure20}, nil
}

func (x *xdbReader) close() { x.mu.Lock(); defer x.mu.Unlock(); x.reader.Close() }
func (x *xdbReader) lookup(a netip.Addr) (Location, error) {
	// The low-level reader shares a file offset and an IO counter.
	x.mu.Lock()
	defer x.mu.Unlock()
	region, err := x.reader.Search(a.String())
	if err != nil {
		return Location{}, err
	}
	return parseXDB(region, x.version, x.legacy)
}

func (s *Service) openLocal() (err error) {
	for _, db := range []struct {
		path, name string
		version    *xdb.Version
		target     **xdbReader
	}{
		{s.config.GeoXDBv4, "GEO_XDB_IPV4", xdb.IPv4, &s.xdb4},
		{s.config.GeoXDBv6, "GEO_XDB_IPV6", xdb.IPv6, &s.xdb6},
	} {
		if db.path == "" {
			continue
		}
		*db.target, err = openXDB(db.path, db.version)
		if err != nil {
			return fmt.Errorf("加载 %s 的 XDB 文件 %q 失败: %w", db.name, db.path, err)
		}
	}
	if s.config.GeoPath != "" {
		s.local, err = maxminddb.Open(s.config.GeoPath)
		if err != nil {
			return fmt.Errorf("加载 GEO_DATABASE 的 MMDB 文件 %q 失败: %w", s.config.GeoPath, err)
		}
	}
	return nil
}

func (s *Service) lookupLocal(a netip.Addr) (Location, error) {
	s.localMu.RLock()
	defer s.localMu.RUnlock()
	if s.closed {
		return Location{}, ErrUnavailable
	}
	var partial Location
	var observations []Evidence
	finish := func(p Location, err error) (Location, error) { p.Evidence = observations; return p, err }
	var lastErr error
	x := s.xdb6
	if a.Is4() {
		x = s.xdb4
	}
	if x != nil {
		p, err := x.lookup(a)
		observations = append(observations, evidence(p, "xdb", err))
		lastErr = err
		if err == nil && p.Country != "" {
			if p.Province != "" {
				return finish(p, nil)
			}
			partial = p
		}
	}
	if s.local != nil {
		p, err := s.lookupMMDB(a)
		observations = append(observations, evidence(p, "mmdb", err))
		lastErr = err
		if err == nil && p.Country != "" {
			if p.Province != "" {
				return finish(p, nil)
			}
			if partial.Country == "" {
				partial = p
			}
		}
	}
	if partial.Country != "" {
		return finish(partial, nil)
	}
	return finish(Location{}, lastErr)
}
