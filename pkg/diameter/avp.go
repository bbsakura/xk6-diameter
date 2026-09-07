package diameter

import (
	"errors"
	"net"
	"strings"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

type AVP struct {
	// TODO: json.UnmarshalJSON() to accept `[{"User-Name": "000000000"}]`
	Key   string
	Value interface{}
}

type AVPMeta struct {
	code   uint32
	flag   uint8
	vendor uint32
	value  datatype.Type
}

func (pair *AVP) modifyMessage(m *diam.Message, meta *smpeer.Metadata) error {
	a, err := makeAVPForMessage(m, pair.Key, pair.Value)
	if err != nil {
		return err
	}
	m.AddAVP(a)
	return nil
}

// makeAVPForMessage builds a *diam.AVP from a (key, value) pair by
// resolving the AVP through the message's dict.Parser: code, vendor
// and flags come from the dictionary entry, and the value is coerced
// to the declared datatype.TypeID via convertByType (or groupedByDict
// for nested Grouped AVPs).
func makeAVPForMessage(m *diam.Message, key string, value any) (*diam.AVP, error) {
	d := m.Dictionary()
	if d == nil {
		return nil, &ErrNotFound{Name: key}
	}
	dAvp, err := d.FindAVP(m.Header.ApplicationID, key)
	if err != nil {
		return nil, &ErrNotFound{Name: key}
	}
	var val datatype.Type
	if dAvp.Data.Type == datatype.GroupedType {
		val, err = groupedByDict(m, value)
	} else {
		val, err = convertByType(dAvp.Data.Type, value)
	}
	if err != nil {
		return nil, err
	}
	return diam.NewAVP(dAvp.Code, dictFlags(dAvp), dAvp.VendorID, val), nil
}

func dictFlags(a *dict.AVP) uint8 {
	var f uint8
	if strings.Contains(a.Must, "M") {
		f |= avp.Mbit
	}
	if a.VendorID != 0 {
		f |= avp.Vbit
	}
	return f
}

// convertByType dispatches on the AVP's declared TypeID to convert a
// JS-supplied value into the corresponding datatype.Type. String-ish
// types accept string, []byte, or JS int arrays (see toStringBytes);
// numeric types accept int64 (or float64 for Float32/64).
func convertByType(t datatype.TypeID, v any) (datatype.Type, error) {
	switch t {
	case datatype.OctetStringType:
		return toOctetString(v)
	case datatype.UTF8StringType:
		return toUTF8String(v)
	case datatype.DiameterIdentityType:
		s, err := toStringBytes(v)
		if err != nil {
			return nil, err
		}
		return datatype.DiameterIdentity(s), nil
	case datatype.DiameterURIType:
		s, err := toStringBytes(v)
		if err != nil {
			return nil, err
		}
		return datatype.DiameterURI(s), nil
	case datatype.IPFilterRuleType:
		s, err := toStringBytes(v)
		if err != nil {
			return nil, err
		}
		return datatype.IPFilterRule(s), nil
	case datatype.QoSFilterRuleType:
		s, err := toStringBytes(v)
		if err != nil {
			return nil, err
		}
		return datatype.QoSFilterRule(s), nil
	case datatype.EnumeratedType:
		return toEnumerated(v)
	case datatype.Integer32Type:
		i, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return datatype.Integer32(i), nil
	case datatype.Integer64Type:
		i, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return datatype.Integer64(i), nil
	case datatype.Unsigned32Type:
		return toUnsigned32(v)
	case datatype.Unsigned64Type:
		i, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return datatype.Unsigned64(uint64(i)), nil
	case datatype.Float32Type:
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return datatype.Float32(f), nil
	case datatype.Float64Type:
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return datatype.Float64(f), nil
	case datatype.TimeType:
		i, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return datatype.Time(time.Unix(i, 0)), nil
	case datatype.AddressType:
		ip, err := toIP(v)
		if err != nil {
			return nil, err
		}
		return datatype.Address(ip), nil
	case datatype.IPv4Type:
		ip, err := toIP(v)
		if err != nil {
			return nil, err
		}
		return datatype.IPv4(ip.To4()), nil
	case datatype.IPv6Type:
		ip, err := toIP(v)
		if err != nil {
			return nil, err
		}
		return datatype.IPv6(ip.To16()), nil
	}
	return nil, &ErrInvalidType{Value: v, Want: "known datatype"}
}

func groupedByDict(m *diam.Message, v any) (datatype.Type, error) {
	items, ok := v.([]interface{})
	if !ok {
		return nil, &ErrInvalidType{Value: v, Want: "[]map[string]any"}
	}
	members := make([]*diam.AVP, 0, len(items))
	for _, item := range items {
		pair, ok := item.(map[string]interface{})
		if !ok {
			return nil, &ErrInvalidType{Value: item, Want: "map[string]any"}
		}
		key, ok := pair["key"].(string)
		if !ok {
			return nil, &ErrInvalidType{Value: pair["key"], Want: "string"}
		}
		value, ok := pair["value"]
		if !ok {
			return nil, &ErrNoValue{Key: key}
		}
		a, err := makeAVPForMessage(m, key, value)
		if err != nil {
			return nil, err
		}
		members = append(members, a)
	}
	return &diam.GroupedAVP{AVP: members}, nil
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case float64:
		return int64(x), nil
	}
	return 0, &ErrInvalidType{Value: v, Want: "int64"}
}

func toFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case int64:
		return float64(x), nil
	}
	return 0, &ErrInvalidType{Value: v, Want: "float64"}
}

func toIP(v any) (net.IP, error) {
	switch x := v.(type) {
	case string:
		ip := net.ParseIP(x)
		if ip == nil {
			return nil, &ErrInvalidType{Value: v, Want: "IP address string"}
		}
		return ip, nil
	case []byte:
		return net.IP(x), nil
	}
	return nil, &ErrInvalidType{Value: v, Want: "IP address string or []byte"}
}

func appendAVPs(m *diam.Message, meta *smpeer.Metadata, avps []AVP) error {
	var errs []error
	for _, pair := range avps {
		err := pair.modifyMessage(m, meta)
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func modifyMessage(m *diam.Message, meta *smpeer.Metadata, options ConnectionOptions) error {
	if options.DestinationHost == nil {
		_, err := m.NewAVP(avp.DestinationHost, avp.Mbit, 0, meta.OriginHost)
		if err != nil {
			return err
		}
	} else if *options.DestinationHost != "" {
		_, err := m.NewAVP(avp.DestinationHost, avp.Mbit, 0, *options.DestinationHost)
		if err != nil {
			return err
		}
	}
	if options.DestinationRealm == nil {
		_, err := m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, meta.OriginRealm)
		if err != nil {
			return err
		}
	} else {
		_, err := m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, *options.DestinationRealm)
		if err != nil {
			return err
		}
	}
	return nil
}
