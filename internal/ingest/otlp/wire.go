package otlp

import (
	"fmt"
	"math"
)

// The protobuf wire format, only as much of it as OTLP needs. Every read is
// bounds checked and every error is returned, never panicked: request bodies
// come from the network.

type wireType uint8

const (
	wireVarint     wireType = 0
	wireFixed64    wireType = 1
	wireBytes      wireType = 2
	wireStartGroup wireType = 3
	wireEndGroup   wireType = 4
	wireFixed32    wireType = 5
)

// maxGroupDepth bounds nested groups while skipping them. OTLP never uses
// groups; they are skipped only because a well-formed message may carry them
// as unknown fields.
const maxGroupDepth = 32

var errTruncated = fmt.Errorf("%w: truncated protobuf", ErrMalformed)

// field is one decoded field. num and typ come from the tag; u holds the
// value of varint, fixed32 and fixed64 fields, b the payload of a
// length-delimited field (a subslice of the input, not a copy).
type field struct {
	num int32
	typ wireType
	u   uint64
	b   []byte
}

func (f field) wrongType() error {
	return fmt.Errorf("%w: field %d has wire type %d", ErrMalformed, f.num, f.typ)
}

// bytesVal returns the payload of a length-delimited field.
func (f field) bytesVal() ([]byte, error) {
	if f.typ != wireBytes {
		return nil, f.wrongType()
	}
	return f.b, nil
}

func (f field) varintVal() (uint64, error) {
	if f.typ != wireVarint {
		return 0, f.wrongType()
	}
	return f.u, nil
}

func (f field) fixed64Val() (uint64, error) {
	if f.typ != wireFixed64 {
		return 0, f.wrongType()
	}
	return f.u, nil
}

func (f field) fixed32Val() (uint32, error) {
	if f.typ != wireFixed32 {
		return 0, f.wrongType()
	}
	return uint32(f.u), nil
}

// readVarint decodes a base-128 varint at b[i:]. It returns the value and
// the index after it.
func readVarint(b []byte, i int) (uint64, int, error) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if i >= len(b) {
			return 0, 0, errTruncated
		}
		c := b[i]
		i++
		// The tenth byte may only contribute the top bit.
		if shift == 63 && c > 1 {
			return 0, 0, fmt.Errorf("%w: varint overflows 64 bits", ErrMalformed)
		}
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, i, nil
		}
	}
	return 0, 0, fmt.Errorf("%w: varint overflows 64 bits", ErrMalformed)
}

// readField decodes the field at b[i:] and returns it with the index after it.
func readField(b []byte, i int) (field, int, error) {
	tag, i, err := readVarint(b, i)
	if err != nil {
		return field{}, 0, err
	}
	num := tag >> 3
	if num == 0 || num > math.MaxInt32 {
		return field{}, 0, fmt.Errorf("%w: invalid field number %d", ErrMalformed, num)
	}
	f := field{num: int32(num), typ: wireType(tag & 7)}
	switch f.typ {
	case wireVarint:
		f.u, i, err = readVarint(b, i)
		if err != nil {
			return field{}, 0, err
		}
	case wireFixed64:
		if len(b)-i < 8 {
			return field{}, 0, errTruncated
		}
		f.u = uint64(b[i]) | uint64(b[i+1])<<8 | uint64(b[i+2])<<16 | uint64(b[i+3])<<24 |
			uint64(b[i+4])<<32 | uint64(b[i+5])<<40 | uint64(b[i+6])<<48 | uint64(b[i+7])<<56
		i += 8
	case wireFixed32:
		if len(b)-i < 4 {
			return field{}, 0, errTruncated
		}
		f.u = uint64(b[i]) | uint64(b[i+1])<<8 | uint64(b[i+2])<<16 | uint64(b[i+3])<<24
		i += 4
	case wireBytes:
		var n uint64
		n, i, err = readVarint(b, i)
		if err != nil {
			return field{}, 0, err
		}
		if n > uint64(len(b)-i) {
			return field{}, 0, errTruncated
		}
		f.b = b[i : i+int(n) : i+int(n)]
		i += int(n)
	case wireStartGroup:
		i, err = skipGroup(b, i, f.num, 1)
		if err != nil {
			return field{}, 0, err
		}
	default:
		// A stray end-group or one of the two undefined wire types.
		return field{}, 0, fmt.Errorf("%w: unexpected wire type %d", ErrMalformed, f.typ)
	}
	return f, i, nil
}

// skipGroup skips past the end-group tag matching num.
func skipGroup(b []byte, i int, num int32, depth int) (int, error) {
	if depth > maxGroupDepth {
		return 0, fmt.Errorf("%w: groups nested too deeply", ErrMalformed)
	}
	for {
		if i >= len(b) {
			return 0, errTruncated
		}
		tag, next, err := readVarint(b, i)
		if err != nil {
			return 0, err
		}
		if wireType(tag&7) == wireEndGroup {
			if int64(tag>>3) != int64(num) {
				return 0, fmt.Errorf("%w: mismatched end group", ErrMalformed)
			}
			return next, nil
		}
		if wireType(tag&7) == wireStartGroup {
			n := tag >> 3
			if n == 0 || n > math.MaxInt32 {
				return 0, fmt.Errorf("%w: invalid field number %d", ErrMalformed, n)
			}
			i, err = skipGroup(b, next, int32(n), depth+1)
			if err != nil {
				return 0, err
			}
			continue
		}
		_, i, err = readField(b, i)
		if err != nil {
			return 0, err
		}
	}
}

// walk calls fn for every field of the message in b, in wire order.
func walk(b []byte, fn func(f field) error) error {
	for i := 0; i < len(b); {
		f, next, err := readField(b, i)
		if err != nil {
			return err
		}
		if f.typ != wireStartGroup {
			if err := fn(f); err != nil {
				return err
			}
		}
		i = next
	}
	return nil
}

// appendVarint, appendTag and appendBytesField are the encoder half used by
// the response helpers.
func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func appendTag(dst []byte, num int32, t wireType) []byte {
	return appendVarint(dst, uint64(num)<<3|uint64(t))
}

func appendBytesField(dst []byte, num int32, b []byte) []byte {
	dst = appendTag(dst, num, wireBytes)
	dst = appendVarint(dst, uint64(len(b)))
	return append(dst, b...)
}
