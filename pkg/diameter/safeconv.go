package diameter

import (
	"fmt"
	"math"
)

// safeUint32 narrows a JS-supplied int64 to uint32, rejecting negatives
// and values above math.MaxUint32. Diameter AVP codes / vendor / app IDs
// are all 32-bit unsigned per RFC 6733; anything outside that range is
// a caller bug.
func safeUint32(v int64) (uint32, error) {
	if v < 0 || v > math.MaxUint32 {
		return 0, fmt.Errorf("value %d out of uint32 range", v)
	}
	return uint32(v), nil
}

// safeInt32 narrows a JS int64 to int32 for AVP types like Enumerated /
// Integer32.
func safeInt32(v int64) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("value %d out of int32 range", v)
	}
	return int32(v), nil
}

// safeUint64 refuses to reinterpret a negative int64 as a huge uint64.
func safeUint64(v int64) (uint64, error) {
	if v < 0 {
		return 0, fmt.Errorf("value %d out of uint64 range", v)
	}
	return uint64(v), nil
}

// completionTimeout caps a caller-supplied "seconds to wait" value to
// something we can multiply by time.Second without overflowing int64.
// Anything above one year is treated as "wait forever-ish" and clamped.
const maxCompletionSeconds = 365 * 24 * 60 * 60 // one year

// completionSeconds converts a uint (JS number) to a bounded int64 count
// of seconds, safe to multiply by time.Second. Values above one year are
// clamped to one year; there is no legitimate load-test use for waiting
// longer than that on a single Diameter Answer.
func completionSeconds(v uint) int64 {
	if v > maxCompletionSeconds {
		return int64(maxCompletionSeconds)
	}
	return int64(v) // #nosec G115 -- bounded above by maxCompletionSeconds
}
