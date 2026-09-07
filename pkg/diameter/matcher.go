package diameter

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// cmdCodeNoMatch is a sentinel Command Code stored in Matcher.CmdCode
// when a JS-supplied string could not be resolved via dict.Default;
// no real Diameter command uses ^uint32(0).
const cmdCodeNoMatch = ^uint32(0)

// Matcher filters incoming Diameter messages. Nil header pointers and
// an empty AVPs slice are wildcards; only present fields participate in
// the AND check. AVPs are matched against the message's top-level AVPs
// by resolving each filter Key through dict.Default and comparing the
// serialized bytes of the datatype-converted values.
type Matcher struct {
	AppID     *uint32
	CmdCode   *uint32
	IsRequest *bool
	AVPs      []AVP

	// compiled is populated by MapToMatcher and used by match() as the
	// hot-path avpFilter implementation. It is nil for Go callers who
	// construct Matcher{AVPs: ...} directly — those fall back to
	// avpFilterMatch (dict lookup per message).
	compiled []compiledAVP
}

// compiledAVP is a MapToMatcher-time-resolved AVP filter. It replaces
// two dict lookups + a Serialize allocation per match with one linear
// scan of msg.AVP + one bytes.Equal against the pre-serialized want.
type compiledAVP struct {
	code   uint32
	vendor uint32
	want   []byte // nil = resolution failed → matches nothing
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
	if m.compiled != nil {
		for _, c := range m.compiled {
			if !c.match(msg) {
				return false
			}
		}
		return true
	}
	for _, f := range m.AVPs {
		if !avpFilterMatch(msg, f) {
			return false
		}
	}
	return true
}

// match reports whether a top-level AVP in msg equals the pre-compiled
// target. Absent AVP or byte mismatch counts as non-match.
func (c compiledAVP) match(msg *diam.Message) bool {
	if c.want == nil {
		return false
	}
	for _, a := range msg.AVP {
		if a.Code == c.code && a.VendorID == c.vendor {
			return bytes.Equal(a.Data.Serialize(), c.want)
		}
	}
	return false
}

// avpFilterMatch is the fallback matcher for Go callers who did not go
// through MapToMatcher; it repeats the same dict resolution +
// serialization on every call. Silent non-match on resolution failure
// so a single unresolvable filter does not throw at receive time.
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

// MapToMatcher parses a JS-supplied options object into a Matcher and
// pre-resolves AVP filters against dict.Default. Present-but-mistyped
// fields are rejected rather than silently ignored, so a typo like
// `app_id: "1"` cannot broaden the matcher into a wildcard that
// consumes unrelated messages.
//
// Keys: app_id, cmd_code, is_request, avps.
//
// cmd_code accepts either a number (raw code) or a string (command
// Name / Short / Short+R|A suffix), resolved via dict.Default. An
// unknown string resolves to cmdCodeNoMatch — the matcher never
// matches, mirroring the behavior of unknown AVP keys in avp filters.
func MapToMatcher(m map[string]interface{}) (Matcher, error) {
	var out Matcher
	if raw, present := m["app_id"]; present {
		v, ok := raw.(int64)
		if !ok {
			return Matcher{}, fmt.Errorf("matcher: app_id must be number, got %T", raw)
		}
		u := uint32(v)
		out.AppID = &u
	}
	if raw, present := m["cmd_code"]; present {
		switch v := raw.(type) {
		case int64:
			u := uint32(v)
			out.CmdCode = &u
		case string:
			if code, ok := resolveCmdName(v); ok {
				out.CmdCode = &code
			} else {
				never := cmdCodeNoMatch
				out.CmdCode = &never
			}
		default:
			return Matcher{}, fmt.Errorf("matcher: cmd_code must be number or string, got %T", raw)
		}
	}
	if raw, present := m["is_request"]; present {
		v, ok := raw.(bool)
		if !ok {
			return Matcher{}, fmt.Errorf("matcher: is_request must be boolean, got %T", raw)
		}
		out.IsRequest = &v
	}
	if raw, present := m["avps"]; present {
		items, ok := raw.([]interface{})
		if !ok {
			return Matcher{}, fmt.Errorf("matcher: avps must be array, got %T", raw)
		}
		for i, item := range items {
			pair, ok := item.(map[string]interface{})
			if !ok {
				return Matcher{}, fmt.Errorf("matcher: avps[%d] must be object, got %T", i, item)
			}
			key, ok := pair["key"].(string)
			if !ok {
				return Matcher{}, fmt.Errorf("matcher: avps[%d].key must be string, got %T", i, pair["key"])
			}
			out.AVPs = append(out.AVPs, AVP{Key: key, Value: pair["value"]})
		}
	}
	var appID uint32
	if out.AppID != nil {
		appID = *out.AppID
	}
	out.compiled = compileAVPs(out.AVPs, appID)
	return out, nil
}

// compileAVPs resolves each AVP filter against dict.Default at
// construction time. Unresolvable filters compile to entries with
// want=nil, which never match.
func compileAVPs(filters []AVP, appID uint32) []compiledAVP {
	if len(filters) == 0 {
		return nil
	}
	out := make([]compiledAVP, len(filters))
	for i, f := range filters {
		dAvp, err := dict.Default.FindAVP(appID, f.Key)
		if err != nil {
			continue
		}
		want, err := convertByType(dAvp.Data.Type, f.Value)
		if err != nil {
			continue
		}
		out[i] = compiledAVP{
			code:   dAvp.Code,
			vendor: dAvp.VendorID,
			want:   want.Serialize(),
		}
	}
	return out
}

// cmdNameIndex is a lazy name → code map over dict.Default.Apps() so
// that string cmd_code resolution is one map lookup instead of an
// O(apps × commands) scan. Populated once; consumers must not mutate
// dict.Default after the first Matcher is built.
var (
	cmdNameOnce  sync.Once
	cmdNameIndex map[string]uint32
)

func buildCmdNameIndex() {
	cmdNameIndex = make(map[string]uint32)
	for _, app := range dict.Default.Apps() {
		for _, cmd := range app.Command {
			cmdNameIndex[cmd.Name] = cmd.Code
			cmdNameIndex[cmd.Short] = cmd.Code
		}
	}
}

// resolveCmdName returns the code for a Command Name or Short. As a
// convenience, unknown names ending in R or A are retried against the
// base short (e.g. "AIR" → "AI"); Request/Answer direction is left to
// is_request.
func resolveCmdName(name string) (uint32, bool) {
	cmdNameOnce.Do(buildCmdNameIndex)
	if code, ok := cmdNameIndex[name]; ok {
		return code, true
	}
	if n := len(name); n > 1 {
		if last := name[n-1]; last == 'R' || last == 'A' {
			if code, ok := cmdNameIndex[name[:n-1]]; ok {
				return code, true
			}
		}
	}
	return 0, false
}
