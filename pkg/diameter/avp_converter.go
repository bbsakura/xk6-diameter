
package diameter

import (
	"errors"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

func toGroupd(v interface{}) (datatype.Type, error) {
	val, ok := v.([]interface{})
	if !ok {
		return nil, errors.New("invalid group value")
	}
	var members []*diam.AVP
	for _, item := range val {
		internalAvp, ok := item.(map[string]interface{})
		if !ok {
			return nil, errors.New("invalid internal item's value")
		}
		key, ok := internalAvp["key"].(string)
		if !ok {
			return nil, errors.New("invalid internal's key value")
		}
		value, ok := internalAvp["value"]
		if !ok {
			return nil, errors.New("invalid internal item (no value field)")
		}

		avpMeta, ok := avpDict[key]
		if !ok {
			return nil, errors.New("invalid value")
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
		return nil, errors.New("invalid value")
	}
	return datatype.UTF8String(val), nil
}

func toOctetString(v interface{}) (datatype.Type, error) {
	val, ok := v.(string)
	if !ok {
		bval, ok := v.([]interface{})
		if !ok {
			return nil, errors.New("invalid value")
		}
		var bites []byte
		for _, in := range bval {
			v := in.(int64)
			bites = append(bites, byte(v))
		}
		val = string(bites)
	}
	return datatype.OctetString(val), nil
}

func toEnumerated(v interface{}) (datatype.Type, error) {
	val, _ := v.(int64)
	// if !ok {
	// 	return nil, errors.New("invalid value")
	// }
	return datatype.Enumerated(val), nil
}
func toUnsigned32(v interface{}) (datatype.Type, error) {
	val, _ := v.(int64)
	// if !ok {
	// 	return nil, errors.New("invalid value")
	// }
	return datatype.Unsigned32(val), nil
}
