package diameter

import (
	"bytes"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
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
//
// cmd_code accepts either a number (raw code) or a string (command
// Name like "Authentication-Information" or Short like "AIR"),
// resolved against dict.Default. An unknown string resolves to a
// sentinel that matches no real command, mirroring the behavior of
// unknown AVP keys in avp filters.
func MapToMatcher(m map[string]interface{}) Matcher {
	var out Matcher
	if v, ok := m["app_id"].(int64); ok {
		u := uint32(v)
		out.AppID = &u
	}
	switch v := m["cmd_code"].(type) {
	case int64:
		u := uint32(v)
		out.CmdCode = &u
	case string:
		if code, ok := resolveCmdName(v); ok {
			out.CmdCode = &code
		} else {
			never := ^uint32(0)
			out.CmdCode = &never
		}
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

// resolveCmdName looks up a command by Name or Short across every App
// registered in dict.Default and returns its code. To support the
// common "AIR" / "AIA" / "ULR" / "ULA" parlance where the trailing
// letter denotes Request/Answer, unknown names ending in R or A are
// retried against the base short (e.g. "AIR" → "AI"); is_request then
// distinguishes the direction.
func resolveCmdName(name string) (uint32, bool) {
	for _, app := range dict.Default.Apps() {
		for _, cmd := range app.Command {
			if cmd.Name == name || cmd.Short == name {
				return cmd.Code, true
			}
		}
	}
	if len(name) > 1 {
		if last := name[len(name)-1]; last == 'R' || last == 'A' {
			base := name[:len(name)-1]
			for _, app := range dict.Default.Apps() {
				for _, cmd := range app.Command {
					if cmd.Short == base {
						return cmd.Code, true
					}
				}
			}
		}
	}
	return 0, false
}
