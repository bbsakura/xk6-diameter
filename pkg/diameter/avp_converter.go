package diameter

import (
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

func toGrouped(v interface{}) (datatype.Type, error) {
	val, ok := v.([]interface{})
	if !ok {
		return nil, &ErrInvalidType{Value: v, Want: "[]map[string]interface{}"}
	}
	var members []*diam.AVP
	for _, item := range val {
		internalAvp, ok := item.(map[string]interface{})
		if !ok {
			return nil, &ErrInvalidType{Value: item, Want: "map[string]interface{}"}
		}
		key, ok := internalAvp["key"].(string)
		if !ok {
			return nil, &ErrInvalidType{Value: internalAvp["key"], Want: "string"}
		}
		value, ok := internalAvp["value"]
		if !ok {
			return nil, &ErrNoValue{key}
		}

		avpMeta, ok := avpDict[key]
		if !ok {
			return nil, &ErrNotFound{key}
		}
		val, err := avpMeta.converter(value)
		if err != nil {
			return nil, err
		}
		members = append(members, diam.NewAVP(avpMeta.code, avpMeta.flag, avpMeta.vendor, val))
	}
	return &diam.GroupedAVP{AVP: members}, nil
}

func toUTF8String(v interface{}) (datatype.Type, error) {
	val, ok := v.(string)
	if !ok {
		v, err := convertInt64SliceToString(v)
		if err != nil {
			return nil, err
		}
		val = v
	}
	return datatype.UTF8String(val), nil
}

func toOctetString(v interface{}) (datatype.Type, error) {
	val, ok := v.(string)
	if !ok {
		v, err := convertInt64SliceToString(v)
		if err != nil {
			return nil, err
		}
		val = v
	}
	return datatype.OctetString(val), nil
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

func convertInt64SliceToString(v interface{}) (string, error) {
	bval, ok := v.([]interface{})
	if !ok {
		return "", &ErrInvalidType{Value: v, Want: "string or []byte"}
	}
	var bites []byte
	for _, in := range bval {
		v := in.(int64)
		bites = append(bites, byte(v))
	}
	return string(bites), nil
}
