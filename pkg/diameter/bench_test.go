package diameter

import (
	"sync"
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// BenchmarkResolveAVP_Cached measures the steady-state cost of the
// dict resolution cache once populated with the AVPs used by S6a
// AIR/ULR. Compare against BenchmarkResolveAVP_FindAVP for the raw
// dict.Parser.FindAVP baseline.
func BenchmarkResolveAVP_Cached(b *testing.B) {
	keys := s6aAVPKeys()
	// Warm the cache.
	for _, k := range keys {
		if _, err := resolveAVP(dict.Default, diam.TGPP_S6A_APP_ID, k); err != nil {
			b.Fatalf("warm resolveAVP(%q): %v", k, err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := resolveAVP(dict.Default, diam.TGPP_S6A_APP_ID, keys[i%len(keys)]); err != nil {
			b.Fatalf("resolveAVP: %v", err)
		}
	}
}

// BenchmarkResolveAVP_FindAVP baselines the un-cached path: what every
// per-send AVP costs today without the cache.
func BenchmarkResolveAVP_FindAVP(b *testing.B) {
	keys := s6aAVPKeys()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := dict.Default.FindAVP(diam.TGPP_S6A_APP_ID, keys[i%len(keys)]); err != nil {
			b.Fatalf("FindAVP: %v", err)
		}
	}
}

// BenchmarkCorrelationTbl exercises the register → deliver → unregister
// triple that every CheckSend* / sendPromise round-trip runs. Kept as a
// baseline for Phase 2 sync.Map experiments.
func BenchmarkCorrelationTbl(b *testing.B) {
	tbl := newCorrelationTbl()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		hbh := uint32(i + 1)
		ch := tbl.register(hbh)
		tbl.deliver(newBenchMsg(hbh))
		<-ch
		tbl.unregister(hbh)
	}
}

// BenchmarkCorrelationTbl_Parallel adds contention: mimics many VUs
// sharing one *Client and racing the correlation table.
func BenchmarkCorrelationTbl_Parallel(b *testing.B) {
	tbl := newCorrelationTbl()
	b.ReportAllocs()
	var seq uint32
	var mu sync.Mutex
	next := func() uint32 {
		mu.Lock()
		seq++
		v := seq
		mu.Unlock()
		return v
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			hbh := next()
			ch := tbl.register(hbh)
			tbl.deliver(newBenchMsg(hbh))
			<-ch
			tbl.unregister(hbh)
		}
	})
}

// BenchmarkGenerateSessionID measures the Phase 1 sid generator; kept
// here for regression detection against the crypto/rand baseline.
func BenchmarkGenerateSessionID(b *testing.B) {
	c := &Client{sidPrefix: "session;deadbeefdeadbeef"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = c.generateSessionID()
	}
}

func s6aAVPKeys() []string {
	return []string{
		"Session-Id",
		"Origin-Host",
		"Origin-Realm",
		"Destination-Realm",
		"Destination-Host",
		"Auth-Session-State",
		"User-Name",
		"Visited-PLMN-Id",
		"Requested-EUTRAN-Authentication-Info",
		"ULR-Flags",
	}
}

func newBenchMsg(hbhID uint32) *diam.Message {
	m := diam.NewMessage(diam.AuthenticationInformation, 0, diam.TGPP_S6A_APP_ID, 0, 0, dict.Default)
	m.Header.HopByHopID = hbhID
	return m
}
