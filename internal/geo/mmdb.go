package geo

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Validation and lookups must decode the same fields, including optional ones.
type mmdbRecord struct {
	Country struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  *float64 `maxminddb:"latitude"`
		Longitude *float64 `maxminddb:"longitude"`
		Accuracy  *uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
	ISP          string `maxminddb:"isp"`
	Organization string `maxminddb:"autonomous_system_organization"`
	ASN          uint   `maxminddb:"autonomous_system_number"`
}

func verifyMMDBRecords(ctx context.Context, path string, r *maxminddb.Reader) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("MMDB 地址索引无法读取")
	}
	defer f.Close()
	// Verify has already checked the tree and its depth. Walk unique nodes,
	// not network prefixes: a tiny shared DAG can represent 2^128 prefixes.
	count, nodeSize := uint64(r.Metadata.NodeCount), int64(r.Metadata.RecordSize/4)
	visited := make([]byte, (count+7)/8)
	stack := []uint64{0}
	// Cache a small window of tree bytes rather than copying the full tree or
	// issuing one file read per node in ordinary contiguous indexes.
	block := make([]byte, 64*1024)
	blockStart, blockEnd := int64(-1), int64(-1)
	uint24 := func(b []byte) uint64 { return uint64(b[0])<<16 | uint64(b[1])<<8 | uint64(b[2]) }
	// Several networks can share a record. Bound the successful-offset cache;
	// uncached records are still decoded after the cache reaches its limit.
	checked := make(map[uintptr]struct{})
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		pointer := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if pointer == count {
			continue // This branch has no record.
		}
		if pointer < count {
			mask := byte(1 << (pointer % 8))
			if visited[pointer/8]&mask != 0 {
				continue
			}
			visited[pointer/8] |= mask
			at := int64(pointer) * nodeSize
			if at < blockStart || at+nodeSize > blockEnd {
				n, err := f.ReadAt(block, at)
				if err != nil && !errors.Is(err, io.EOF) || int64(n) < nodeSize {
					return errors.New("MMDB 地址索引读取失败")
				}
				blockStart, blockEnd = at, at+int64(n)
			}
			node := block[at-blockStart : at-blockStart+nodeSize]
			var left, right uint64
			switch r.Metadata.RecordSize {
			case 24:
				left, right = uint24(node[:3]), uint24(node[3:])
			case 28:
				left = uint24(node[:3]) | uint64(node[3]&0xf0)<<20
				right = uint24(node[4:]) | uint64(node[3]&0x0f)<<24
			case 32:
				left, right = uint64(binary.BigEndian.Uint32(node[:4])), uint64(binary.BigEndian.Uint32(node[4:]))
			default:
				return errors.New("MMDB 地址索引位数无效")
			}
			stack = append(stack, right, left)
			continue
		}
		// Data pointers include the node count and the 16-byte separator.
		if pointer < count+16 {
			return errors.New("MMDB 记录指针无效")
		}
		offset := uintptr(pointer - count - 16)
		if _, ok := checked[offset]; ok {
			continue
		}
		var record mmdbRecord
		if err := r.LookupOffset(offset).Decode(&record); err != nil {
			return errors.New("MMDB City 记录字段类型或内容无效")
		}
		if len(checked) < 64*1024 {
			checked[offset] = struct{}{}
		}
	}
	return ctx.Err()
}
