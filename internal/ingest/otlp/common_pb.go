package otlp

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
)

// Field numbers from opentelemetry/proto/common/v1/common.proto and
// opentelemetry/proto/resource/v1/resource.proto.
const (
	fAnyStringValue = 1 // AnyValue.string_value
	fAnyBoolValue   = 2 // AnyValue.bool_value
	fAnyIntValue    = 3 // AnyValue.int_value (int64)
	fAnyDoubleValue = 4 // AnyValue.double_value
	fAnyArrayValue  = 5 // AnyValue.array_value
	fAnyKvlistValue = 6 // AnyValue.kvlist_value
	fAnyBytesValue  = 7 // AnyValue.bytes_value

	fArrayValueValues   = 1 // ArrayValue.values
	fKeyValueListValues = 1 // KeyValueList.values

	fKeyValueKey   = 1 // KeyValue.key
	fKeyValueValue = 2 // KeyValue.value

	fScopeName       = 1 // InstrumentationScope.name
	fScopeVersion    = 2 // InstrumentationScope.version
	fScopeAttributes = 3 // InstrumentationScope.attributes

	fResourceAttributes = 1 // Resource.attributes
)

// pbAny is a decoded AnyValue. kind is the oneof field number set last on
// the wire (0 when none is), u holds bool, int and double bits, b the
// payload of string, bytes, array and kvlist.
type pbAny struct {
	kind int32
	u    uint64
	b    []byte
}

func parseAny(b []byte) (pbAny, error) {
	var a pbAny
	err := walk(b, func(f field) error {
		var err error
		switch f.num {
		case fAnyStringValue, fAnyBytesValue, fAnyArrayValue, fAnyKvlistValue:
			a.b, err = f.bytesVal()
			a.u = 0
		case fAnyBoolValue, fAnyIntValue:
			a.u, err = f.varintVal()
			a.b = nil
		case fAnyDoubleValue:
			a.u, err = f.fixed64Val()
			a.b = nil
		default:
			return nil
		}
		a.kind = f.num
		return err
	})
	return a, err
}

// pbAnyString renders an AnyValue the way pcommon Value.AsString does,
// which is what the exporter stores for attributes and Body, cut to limit
// bytes.
func (d *decoder) pbAnyString(b []byte, limit int) (string, error) {
	a, err := parseAny(b)
	if err != nil {
		return "", err
	}
	switch a.kind {
	case 0:
		return "", nil
	case fAnyStringValue:
		return string(truncate(a.b, limit)), nil
	case fAnyBoolValue:
		return strconv.FormatBool(a.u != 0), nil
	case fAnyIntValue:
		return strconv.FormatInt(int64(a.u), 10), nil
	case fAnyDoubleValue:
		f := math.Float64frombits(a.u)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return strconv.FormatFloat(f, 'g', -1, 64), nil
		}
		return string(appendFloat(nil, f)), nil
	case fAnyBytesValue:
		return truncate(base64.StdEncoding.EncodeToString(a.b), limit), nil
	}
	out, err := d.appendPbAnyJSON(nil, a, 0)
	return string(truncate(out, limit)), err
}

// appendPbAnyJSON writes an AnyValue as JSON. Arrays and maps nest up to
// Limits.MaxDepth.
func (d *decoder) appendPbAnyJSON(dst []byte, a pbAny, depth int) ([]byte, error) {
	switch a.kind {
	case 0:
		return append(dst, "null"...), nil
	case fAnyStringValue:
		return appendJSONString(dst, string(a.b)), nil
	case fAnyBoolValue:
		return strconv.AppendBool(dst, a.u != 0), nil
	case fAnyIntValue:
		return strconv.AppendInt(dst, int64(a.u), 10), nil
	case fAnyDoubleValue:
		f := math.Float64frombits(a.u)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return append(dst, "null"...), nil
		}
		return appendFloat(dst, f), nil
	case fAnyBytesValue:
		dst = append(dst, '"')
		dst = base64.StdEncoding.AppendEncode(dst, a.b)
		return append(dst, '"'), nil
	}
	if depth >= d.lim.MaxDepth {
		return nil, fmt.Errorf("%w: attribute value nested deeper than %d", ErrMalformed, d.lim.MaxDepth)
	}
	if a.kind == fAnyArrayValue {
		dst = append(dst, '[')
		first := true
		err := walk(a.b, func(f field) error {
			if f.num != fArrayValueValues {
				return nil
			}
			vb, err := f.bytesVal()
			if err != nil {
				return err
			}
			v, err := parseAny(vb)
			if err != nil {
				return err
			}
			if !first {
				dst = append(dst, ',')
			}
			first = false
			dst, err = d.appendPbAnyJSON(dst, v, depth+1)
			return err
		})
		if err != nil {
			return nil, err
		}
		return append(dst, ']'), nil
	}
	// kvlist
	var entries []kvJSON
	err := walk(a.b, func(f field) error {
		if f.num != fKeyValueListValues {
			return nil
		}
		kvb, err := f.bytesVal()
		if err != nil {
			return err
		}
		key, vb, err := parseKeyValue(kvb)
		if err != nil {
			return err
		}
		v, err := parseAny(vb)
		if err != nil {
			return err
		}
		val, err := d.appendPbAnyJSON(nil, v, depth+1)
		if err != nil {
			return err
		}
		entries = append(entries, kvJSON{key: string(key), val: val})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return appendKVObject(dst, entries), nil
}

// parseKeyValue returns the raw key and the AnyValue payload of a KeyValue.
func parseKeyValue(b []byte) (key, value []byte, err error) {
	err = walk(b, func(f field) error {
		var err error
		switch f.num {
		case fKeyValueKey:
			key, err = f.bytesVal()
		case fKeyValueValue:
			value, err = f.bytesVal()
		}
		return err
	})
	return key, value, err
}

// pbAttr adds one KeyValue to set.
func (d *decoder) pbAttr(set *attrSet, kv []byte) error {
	kb, vb, err := parseKeyValue(kv)
	if err != nil {
		return err
	}
	key := string(truncate(kb, d.lim.MaxValueBytes))
	if !set.has(key) {
		return nil
	}
	val, err := d.pbAnyString(vb, d.lim.MaxValueBytes)
	if err != nil {
		return err
	}
	set.put(key, val)
	return nil
}

// pbScope is a decoded InstrumentationScope.
type pbScope struct {
	name, version string
	attrs         map[string]string
}

func (d *decoder) parseScope(b []byte) (pbScope, error) {
	s := pbScope{}
	set := newAttrSet(&d.lim)
	err := walk(b, func(f field) error {
		switch f.num {
		case fScopeName:
			v, err := f.bytesVal()
			s.name = d.str(v)
			return err
		case fScopeVersion:
			v, err := f.bytesVal()
			s.version = d.str(v)
			return err
		case fScopeAttributes:
			v, err := f.bytesVal()
			if err != nil {
				return err
			}
			return d.pbAttr(&set, v)
		}
		return nil
	})
	s.attrs = set.m
	return s, err
}

func (d *decoder) parseResource(b []byte) (map[string]string, error) {
	set := newAttrSet(&d.lim)
	err := walk(b, func(f field) error {
		if f.num != fResourceAttributes {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		return d.pbAttr(&set, v)
	})
	return set.m, err
}

// str copies a string field, cut to MaxValueBytes.
func (d *decoder) str(b []byte) string {
	return string(truncate(b, d.lim.MaxValueBytes))
}
