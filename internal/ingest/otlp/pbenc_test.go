package otlp

import (
	"encoding/binary"
	"math"
)

// pb is a tiny test-only protobuf encoder. Each method appends one field.
type pb []byte

func (p pb) varint(num int32, v uint64) pb {
	b := appendTag([]byte(p), num, wireVarint)
	return pb(appendVarint(b, v))
}

func (p pb) fixed64(num int32, v uint64) pb {
	b := appendTag([]byte(p), num, wireFixed64)
	return pb(binary.LittleEndian.AppendUint64(b, v))
}

func (p pb) fixed32(num int32, v uint32) pb {
	b := appendTag([]byte(p), num, wireFixed32)
	return pb(binary.LittleEndian.AppendUint32(b, v))
}

func (p pb) bytes(num int32, v []byte) pb { return pb(appendBytesField([]byte(p), num, v)) }

func (p pb) str(num int32, s string) pb { return p.bytes(num, []byte(s)) }

func (p pb) msg(num int32, m pb) pb { return p.bytes(num, []byte(m)) }

// group appends a start group, inner fields and the matching end group.
func (p pb) group(num int32, inner pb) pb {
	b := appendTag([]byte(p), num, wireStartGroup)
	b = append(b, inner...)
	return pb(appendTag(b, num, wireEndGroup))
}

// unknowns appends one unknown field of every wire type, numbered from 900.
func (p pb) unknowns() pb {
	return p.varint(900, 12345).fixed64(901, 7).fixed32(902, 9).str(903, "junk").
		group(904, pb(nil).varint(1, 1).group(2, pb(nil).str(3, "x")))
}

func anyStr(s string) pb     { return pb(nil).str(fAnyStringValue, s) }
func anyInt(v int64) pb      { return pb(nil).varint(fAnyIntValue, uint64(v)) }
func anyBytes(b []byte) pb   { return pb(nil).bytes(fAnyBytesValue, b) }
func anyDouble(f float64) pb { return pb(nil).fixed64(fAnyDoubleValue, math.Float64bits(f)) }
func anyArray(vals ...pb) pb {
	var arr pb
	for _, v := range vals {
		arr = arr.msg(fArrayValueValues, v)
	}
	return pb(nil).msg(fAnyArrayValue, arr)
}

func anyBool(b bool) pb {
	v := uint64(0)
	if b {
		v = 1
	}
	return pb(nil).varint(fAnyBoolValue, v)
}

func anyKV(kvs ...pb) pb {
	var list pb
	for _, kv := range kvs {
		list = list.msg(fKeyValueListValues, kv)
	}
	return pb(nil).msg(fAnyKvlistValue, list)
}

func kv(key string, value pb) pb {
	return pb(nil).str(fKeyValueKey, key).msg(fKeyValueValue, value)
}
