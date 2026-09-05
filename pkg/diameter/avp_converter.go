package diameter

import (
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

func toUTF8String(v interface{}) (datatype.Type, error) {
	s, err := toStringBytes(v)
	if err != nil {
		return nil, err
	}
	return datatype.UTF8String(s), nil
}

func toOctetString(v interface{}) (datatype.Type, error) {
	s, err := toStringBytes(v)
	if err != nil {
		return nil, err
	}
	return datatype.OctetString(s), nil
}

func toEnumerated(v interface{}) (datatype.Type, error) {
	val, ok := v.(int64)
	if !ok {
		return nil, &ErrInvalidType{Value: v, Want: "int32"}
	}
	return datatype.Enumerated(val), nil
}

func toUnsigned32(v interface{}) (datatype.Type, error) {
	val, ok := v.(int64)
	if !ok {
		return nil, &ErrInvalidType{Value: v, Want: "uint32"}
	}
	return datatype.Unsigned32(val), nil
}

// toStringBytes accepts the three forms sobek surfaces for byte-ish data:
// a JS string (Go string), a Go []byte (from Uint8Array/ArrayBuffer via
// sobek Runtime.NewArrayBuffer paths), or a JS array of small integers
// like `[0x05, 0x0a]` which sobek exports as []interface{} of int64.
func toStringBytes(v interface{}) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case []byte:
		return string(x), nil
	case []interface{}:
		return convertInt64SliceToString(x)
	}
	return "", &ErrInvalidType{Value: v, Want: "string, []byte, or []int"}
}

func convertInt64SliceToString(bval []interface{}) (string, error) {
	bites := make([]byte, 0, len(bval))
	for _, in := range bval {
		v, ok := in.(int64)
		if !ok {
			return "", &ErrInvalidType{Value: in, Want: "int64"}
		}
		bites = append(bites, byte(v))
	}
	return string(bites), nil
}
