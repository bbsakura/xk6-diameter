package diameter

import (
	"reflect"
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

func TestDictFlags(t *testing.T) {
	cases := []struct {
		name string
		must string
		vend uint32
		want uint8
	}{
		{"none", "", 0, 0},
		{"m only", "M", 0, avp.Mbit},
		{"v only via vendor", "", 10415, avp.Vbit},
		{"m + v via vendor", "M", 10415, avp.Mbit | avp.Vbit},
		{"m + v spelled out", "M,V", 10415, avp.Mbit | avp.Vbit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dictFlags(&dict.AVP{Must: tc.must, VendorID: tc.vend})
			if got != tc.want {
				t.Fatalf("dictFlags(Must=%q, Vendor=%d) = %#x; want %#x", tc.must, tc.vend, got, tc.want)
			}
		})
	}
}

func TestToStringBytes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hello", "hello"},
		{"byte slice", []byte{'a', 'b'}, "ab"},
		{"js int array", []interface{}{int64(0x05), int64(0x0a)}, "\x05\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toStringBytes(tc.in)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestToStringBytes_Rejects(t *testing.T) {
	if _, err := toStringBytes(123); err == nil {
		t.Fatalf("expected error for int input")
	}
	if _, err := toStringBytes([]interface{}{"not-int"}); err == nil {
		t.Fatalf("expected error for []interface{}{string}")
	}
	if _, err := toStringBytes([]interface{}{int64(-1)}); err == nil {
		t.Fatalf("expected error for byte value -1")
	}
	if _, err := toStringBytes([]interface{}{int64(256)}); err == nil {
		t.Fatalf("expected error for byte value 256")
	}
}

func TestConvertByType(t *testing.T) {
	cases := []struct {
		name string
		t    datatype.TypeID
		in   any
		want datatype.Type
	}{
		{"UTF8String", datatype.UTF8StringType, "abc", datatype.UTF8String("abc")},
		{"OctetString from bytes", datatype.OctetStringType, []byte{1, 2}, datatype.OctetString("\x01\x02")},
		{"OctetString from js array", datatype.OctetStringType, []interface{}{int64(5)}, datatype.OctetString("\x05")},
		{"DiameterIdentity", datatype.DiameterIdentityType, "host.example", datatype.DiameterIdentity("host.example")},
		{"Enumerated", datatype.EnumeratedType, int64(3), datatype.Enumerated(3)},
		{"Unsigned32", datatype.Unsigned32Type, int64(42), datatype.Unsigned32(42)},
		{"Unsigned64 from int64", datatype.Unsigned64Type, int64(1 << 40), datatype.Unsigned64(1 << 40)},
		{"Integer32", datatype.Integer32Type, int64(-7), datatype.Integer32(-7)},
		{"Float64", datatype.Float64Type, 1.5, datatype.Float64(1.5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertByType(tc.t, tc.in)
			if err != nil {
				t.Fatalf("convertByType: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestConvertByType_TypeMismatch(t *testing.T) {
	if _, err := convertByType(datatype.Unsigned32Type, "not-int"); err == nil {
		t.Fatalf("expected error for string→Unsigned32")
	}
	if _, err := convertByType(datatype.UnknownType, "x"); err == nil {
		t.Fatalf("expected error for UnknownType")
	}
}

func TestMakeAVPForMessage_SimpleAVP(t *testing.T) {
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	a, err := makeAVPForMessage(m, "Origin-Host", "host.example.com")
	if err != nil {
		t.Fatalf("makeAVPForMessage: %v", err)
	}
	if a.Code != avp.OriginHost {
		t.Fatalf("Code = %d; want %d", a.Code, avp.OriginHost)
	}
	if a.Flags&avp.Mbit == 0 {
		t.Fatalf("expected Mbit set (Origin-Host is M-must); flags=%#x", a.Flags)
	}
	if a.VendorID != 0 {
		t.Fatalf("VendorID = %d; want 0", a.VendorID)
	}
	if got := a.Data.(datatype.DiameterIdentity); got != "host.example.com" {
		t.Fatalf("Data = %q; want %q", got, "host.example.com")
	}
}

func TestMakeAVPForMessage_VendorAVP(t *testing.T) {
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	a, err := makeAVPForMessage(m, "Visited-PLMN-Id", []byte{0x00, 0xF1, 0x10})
	if err != nil {
		t.Fatalf("makeAVPForMessage: %v", err)
	}
	if a.VendorID != 10415 {
		t.Fatalf("VendorID = %d; want 10415", a.VendorID)
	}
	if a.Flags&(avp.Mbit|avp.Vbit) == 0 {
		t.Fatalf("expected M+V bits set; flags=%#x", a.Flags)
	}
	if got := a.Data.(datatype.OctetString); string(got) != "\x00\xF1\x10" {
		t.Fatalf("Data = %q; want %q", string(got), "\x00\xF1\x10")
	}
}

func TestMakeAVPForMessage_Unknown(t *testing.T) {
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	if _, err := makeAVPForMessage(m, "No-Such-AVP", "x"); err == nil {
		t.Fatalf("expected ErrNotFound for unknown AVP")
	}
}

func TestGroupedByDict_Recursive(t *testing.T) {
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	a, err := makeAVPForMessage(m, "Requested-EUTRAN-Authentication-Info", []interface{}{
		map[string]interface{}{"key": "Number-Of-Requested-Vectors", "value": int64(3)},
		map[string]interface{}{"key": "Immediate-Response-Preferred", "value": int64(0)},
	})
	if err != nil {
		t.Fatalf("makeAVPForMessage grouped: %v", err)
	}
	grp, ok := a.Data.(*diam.GroupedAVP)
	if !ok {
		t.Fatalf("Data is not *diam.GroupedAVP: %T", a.Data)
	}
	if len(grp.AVP) != 2 {
		t.Fatalf("nested AVP count = %d; want 2", len(grp.AVP))
	}
	if got := grp.AVP[0].Data.(datatype.Unsigned32); got != 3 {
		t.Fatalf("first nested Data = %d; want 3", got)
	}
}

func TestGroupedByDict_RejectsMalformed(t *testing.T) {
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	// Missing "value"
	_, err := groupedByDict(m, []interface{}{map[string]interface{}{"key": "IMEI"}})
	if err == nil {
		t.Fatalf("expected ErrNoValue")
	}
	// Not an array
	_, err = groupedByDict(m, "not-an-array")
	if err == nil {
		t.Fatalf("expected ErrInvalidType")
	}
}
