package diameter

import (
	"testing"
)

func TestMapToConnectionOptions_AcceptsUint32Range(t *testing.T) {
	co, err := MapToConnectionOptions(map[string]interface{}{
		"vendor_id": int64(10415),
		"app_id":    int64(4294967295),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.VendorId != 10415 || co.AppId != 4294967295 {
		t.Fatalf("got vendor_id=%d app_id=%d", co.VendorId, co.AppId)
	}
}

func TestMapToConnectionOptions_RejectsInvalidNumbers(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"vendor_id above uint32", "vendor_id", int64(4294967296)},
		{"negative app_id", "app_id", int64(-1)},
		{"non-integer retries", "retries", 1.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MapToConnectionOptions(map[string]interface{}{tc.key: tc.value}); err == nil {
				t.Fatalf("expected error for %s=%v", tc.key, tc.value)
			}
		})
	}
}

func TestMapToConnectionOptions_ParsesTLS(t *testing.T) {
	co, err := MapToConnectionOptions(map[string]interface{}{
		"tls": map[string]interface{}{"enable": true, "cert_file": "c.crt", "key_file": "c.key"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := TLSOptions{Enable: true, CertFile: "c.crt", KeyFile: "c.key"}
	if co.TLS == nil || *co.TLS != want {
		t.Fatalf("TLS = %+v; want %+v", co.TLS, want)
	}
}

func TestMapToConnectionOptions_RejectsMalformedTLS(t *testing.T) {
	cases := []struct {
		name string
		tls  interface{}
	}{
		{"tls is not an object", true},
		{"enable is a string", map[string]interface{}{"enable": "true"}},
		{"cert_file is not a string", map[string]interface{}{"enable": true, "cert_file": int64(1), "key_file": "c.key"}},
		{"key_file without cert_file", map[string]interface{}{"enable": true, "key_file": "c.key"}},
		{"cert_file without key_file", map[string]interface{}{"enable": true, "cert_file": "c.crt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MapToConnectionOptions(map[string]interface{}{"tls": tc.tls}); err == nil {
				t.Fatalf("expected error for tls=%v", tc.tls)
			}
		})
	}
}
