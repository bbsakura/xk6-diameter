package diameter

import (
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

func ptrU32(v uint32) *uint32 { return &v }
func ptrBool(v bool) *bool    { return &v }

// buildTestRequest constructs an AIR-shaped Request with the given AVPs
// attached, using dict.Default so matcher AVP lookups resolve.
func buildTestRequest(t *testing.T, avps ...*diam.AVP) *diam.Message {
	t.Helper()
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	for _, a := range avps {
		m.AddAVP(a)
	}
	return m
}

func TestMatcher_Empty_MatchesAnything(t *testing.T) {
	m := buildTestRequest(t)
	if !(Matcher{}).match(m) {
		t.Fatalf("empty Matcher should match any message")
	}
}

func TestMatcher_Header(t *testing.T) {
	req := buildTestRequest(t)
	ans := diam.NewMessage(diam.AuthenticationInformation, 0, diam.TGPP_S6A_APP_ID, 0, 0, dict.Default)

	cases := []struct {
		name string
		m    Matcher
		msg  *diam.Message
		want bool
	}{
		{"AppID hit", Matcher{AppID: ptrU32(diam.TGPP_S6A_APP_ID)}, req, true},
		{"AppID miss", Matcher{AppID: ptrU32(999)}, req, false},
		{"CmdCode hit", Matcher{CmdCode: ptrU32(diam.AuthenticationInformation)}, req, true},
		{"CmdCode miss", Matcher{CmdCode: ptrU32(999)}, req, false},
		{"IsRequest=true on req", Matcher{IsRequest: ptrBool(true)}, req, true},
		{"IsRequest=false on req", Matcher{IsRequest: ptrBool(false)}, req, false},
		{"IsRequest=false on ans", Matcher{IsRequest: ptrBool(false)}, ans, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.match(tc.msg); got != tc.want {
				t.Fatalf("match=%v; want %v", got, tc.want)
			}
		})
	}
}

func TestMatcher_AVP_UTF8String(t *testing.T) {
	m := buildTestRequest(t,
		diam.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String("001010000000001")),
	)
	if !(Matcher{AVPs: []AVP{{Key: "User-Name", Value: "001010000000001"}}}).match(m) {
		t.Fatalf("expected User-Name match")
	}
	if (Matcher{AVPs: []AVP{{Key: "User-Name", Value: "wrong"}}}).match(m) {
		t.Fatalf("expected User-Name mismatch")
	}
}

func TestMatcher_AVP_Vendor_OctetString(t *testing.T) {
	m := buildTestRequest(t,
		diam.NewAVP(avp.VisitedPLMNID, avp.Mbit|avp.Vbit, 10415, datatype.OctetString("\x00\xF1\x10")),
	)
	if !(Matcher{AVPs: []AVP{{Key: "Visited-PLMN-Id", Value: []byte{0x00, 0xF1, 0x10}}}}).match(m) {
		t.Fatalf("expected Visited-PLMN-Id match via []byte")
	}
	if !(Matcher{AVPs: []AVP{{Key: "Visited-PLMN-Id", Value: []interface{}{int64(0x00), int64(0xF1), int64(0x10)}}}}).match(m) {
		t.Fatalf("expected Visited-PLMN-Id match via JS int array")
	}
}

func TestMatcher_AVP_Enumerated(t *testing.T) {
	m := buildTestRequest(t,
		diam.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Enumerated(1)),
	)
	if !(Matcher{AVPs: []AVP{{Key: "Auth-Session-State", Value: int64(1)}}}).match(m) {
		t.Fatalf("expected Auth-Session-State=1 match")
	}
	if (Matcher{AVPs: []AVP{{Key: "Auth-Session-State", Value: int64(2)}}}).match(m) {
		t.Fatalf("expected Auth-Session-State=2 non-match")
	}
}

func TestMatcher_AVP_Absent(t *testing.T) {
	m := buildTestRequest(t) // no User-Name AVP
	if (Matcher{AVPs: []AVP{{Key: "User-Name", Value: "x"}}}).match(m) {
		t.Fatalf("absent AVP should not match")
	}
}

func TestMatcher_AVP_UnknownKey(t *testing.T) {
	m := buildTestRequest(t)
	if (Matcher{AVPs: []AVP{{Key: "No-Such-AVP", Value: "x"}}}).match(m) {
		t.Fatalf("unknown AVP key should not match")
	}
}

func TestMatcher_AVP_MultipleAND(t *testing.T) {
	m := buildTestRequest(t,
		diam.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String("u")),
		diam.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Enumerated(1)),
	)
	all := Matcher{AVPs: []AVP{
		{Key: "User-Name", Value: "u"},
		{Key: "Auth-Session-State", Value: int64(1)},
	}}
	if !all.match(m) {
		t.Fatalf("AND: both should match")
	}
	one := Matcher{AVPs: []AVP{
		{Key: "User-Name", Value: "u"},
		{Key: "Auth-Session-State", Value: int64(999)},
	}}
	if one.match(m) {
		t.Fatalf("AND: single miss should reject")
	}
}

func TestMatcher_Header_AND_AVP(t *testing.T) {
	m := buildTestRequest(t,
		diam.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String("u")),
	)
	ok := Matcher{
		AppID:   ptrU32(diam.TGPP_S6A_APP_ID),
		CmdCode: ptrU32(diam.AuthenticationInformation),
		AVPs:    []AVP{{Key: "User-Name", Value: "u"}},
	}
	if !ok.match(m) {
		t.Fatalf("expected combined header+AVP match")
	}
}

func TestMapToMatcher(t *testing.T) {
	m := MapToMatcher(map[string]interface{}{
		"app_id":     int64(diam.TGPP_S6A_APP_ID),
		"cmd_code":   int64(diam.AuthenticationInformation),
		"is_request": true,
		"avps": []interface{}{
			map[string]interface{}{"key": "User-Name", "value": "u"},
		},
	})
	if m.AppID == nil || *m.AppID != diam.TGPP_S6A_APP_ID {
		t.Fatalf("AppID not parsed")
	}
	if m.CmdCode == nil || *m.CmdCode != diam.AuthenticationInformation {
		t.Fatalf("CmdCode not parsed")
	}
	if m.IsRequest == nil || !*m.IsRequest {
		t.Fatalf("IsRequest not parsed")
	}
	if len(m.AVPs) != 1 || m.AVPs[0].Key != "User-Name" || m.AVPs[0].Value != "u" {
		t.Fatalf("AVPs not parsed: %+v", m.AVPs)
	}
}

func TestMapToMatcher_Empty(t *testing.T) {
	m := MapToMatcher(map[string]interface{}{})
	if m.AppID != nil || m.CmdCode != nil || m.IsRequest != nil || len(m.AVPs) != 0 {
		t.Fatalf("expected all-wildcard matcher, got %+v", m)
	}
}
