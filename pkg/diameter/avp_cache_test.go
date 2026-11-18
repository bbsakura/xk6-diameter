package diameter

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam/dict"
)

const minimalDictXML = `<?xml version="1.0" encoding="UTF-8"?>
<diameter>
	<application id="0" name="Base">
		<avp name="Origin-Host" code="264" must="M" may="P" must-not="V" may-encrypt="-">
			<data type="DiameterIdentity"/>
		</avp>
	</application>
</diameter>`

func avpResolveCacheLen() int {
	n := 0
	avpResolveCache.Range(func(_, _ any) bool { n++; return true })
	return n
}

func TestResolveAVP_ReleasesCacheWhenParserIsCollected(t *testing.T) {
	before := avpResolveCacheLen()
	func() {
		p, err := dict.NewParser()
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Load(strings.NewReader(minimalDictXML)); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveAVP(p, 0, "Origin-Host"); err != nil {
			t.Fatalf("resolveAVP: %v", err)
		}
		if got := avpResolveCacheLen(); got != before+1 {
			t.Fatalf("cache entries = %d, want %d", got, before+1)
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for avpResolveCacheLen() != before {
		if time.Now().After(deadline) {
			t.Fatalf("cache entries = %d after parser became unreachable, want %d", avpResolveCacheLen(), before)
		}
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}
