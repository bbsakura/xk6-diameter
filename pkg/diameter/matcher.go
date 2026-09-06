package diameter

import (
	"bytes"

	"github.com/fiorix/go-diameter/v4/diam"
)

// Matcher filters incoming Diameter messages. Nil header pointers and
// an empty AVPs slice are wildcards; only present fields participate in
// the AND check. AVPs are matched against the message's top-level AVPs
// by resolving each filter Key through the message's dictionary and
// comparing the serialized bytes of the datatype-converted values.
type Matcher struct {
	AppID     *uint32
	CmdCode   *uint32
	IsRequest *bool
	AVPs      []AVP
}

// match reports whether m accepts msg.
func (m Matcher) match(msg *diam.Message) bool {
	if m.AppID != nil && *m.AppID != msg.Header.ApplicationID {
		return false
	}
	if m.CmdCode != nil && *m.CmdCode != msg.Header.CommandCode {
		return false
	}
	if m.IsRequest != nil {
		isReq := msg.Header.CommandFlags&diam.RequestFlag != 0
		if isReq != *m.IsRequest {
			return false
		}
	}
	for _, f := range m.AVPs {
		if !avpFilterMatch(msg, f) {
			return false
		}
	}
	return true
}

// avpFilterMatch resolves the filter's Key via the message's
// dict.Parser, converts the filter's Value to the AVP's declared
// datatype via convertByType (same path Send* uses), then compares the
// two AVPs' serialized bytes.
//
// Any missing dictionary entry, missing AVP in the message, or value
// coercion failure counts as non-match — matcher construction is
// separated from parse-time validation on purpose so a single
// unresolvable filter does not throw at receive time.
func avpFilterMatch(msg *diam.Message, f AVP) bool {
	d := msg.Dictionary()
	if d == nil {
		return false
	}
	dAvp, err := d.FindAVP(msg.Header.ApplicationID, f.Key)
	if err != nil {
		return false
	}
	got, err := msg.FindAVP(dAvp.Code, dAvp.VendorID)
	if err != nil {
		return false
	}
	want, err := convertByType(dAvp.Data.Type, f.Value)
	if err != nil {
		return false
	}
	return bytes.Equal(got.Data.Serialize(), want.Serialize())
}

// MapToMatcher parses a JS-supplied options object into a Matcher.
// Keys: app_id, cmd_code, is_request, avps.
func MapToMatcher(m map[string]interface{}) Matcher {
	var out Matcher
	if v, ok := m["app_id"].(int64); ok {
		u := uint32(v)
		out.AppID = &u
	}
	if v, ok := m["cmd_code"].(int64); ok {
		u := uint32(v)
		out.CmdCode = &u
	}
	if v, ok := m["is_request"].(bool); ok {
		out.IsRequest = &v
	}
	if v, ok := m["avps"].([]interface{}); ok {
		for _, item := range v {
			pair, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			key, _ := pair["key"].(string)
			out.AVPs = append(out.AVPs, AVP{Key: key, Value: pair["value"]})
		}
	}
	return out
}
