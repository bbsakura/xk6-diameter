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
