package diameter

import (
	"sync"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/diamtest"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
)

const testVendor3GPP = 10415

// newTestHSS starts an in-process TCP Diameter server that answers S6a
// AIR/ULR with Result-Code=2001. Answers echo the request's HBH/E2E ids
// via m.Answer, so mis-correlation is directly observable.
func newTestHSS(t *testing.T, delay time.Duration) *diamtest.Server {
	t.Helper()
	settings := &sm.Settings{
		OriginHost:       "hss.test",
		OriginRealm:      "test.realm",
		VendorID:         testVendor3GPP,
		ProductName:      "xk6-diameter-test",
		FirmwareRevision: 1,
	}
	mux := sm.New(settings)
	mux.HandleIdx(
		diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.AuthenticationInformation, Request: true},
		diam.HandlerFunc(func(c diam.Conn, m *diam.Message) {
			if delay > 0 {
				time.Sleep(delay)
			}
			answer(c, m, settings, diam.Success)
		}))
	mux.HandleIdx(
		diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.UpdateLocation, Request: true},
		diam.HandlerFunc(func(c diam.Conn, m *diam.Message) {
			answer(c, m, settings, diam.Success)
		}))
	return diamtest.NewServer(mux, dict.Default)
}

func answer(c diam.Conn, m *diam.Message, s *sm.Settings, code uint32) {
	a := m.Answer(code)
	var sid datatype.UTF8String
	for _, x := range m.AVP {
		if x.Code == avp.SessionID {
			sid = x.Data.(datatype.UTF8String)
			break
		}
	}
	a.InsertAVP(diam.NewAVP(avp.SessionID, avp.Mbit, 0, sid))
	a.NewAVP(avp.OriginHost, avp.Mbit, 0, s.OriginHost)
	a.NewAVP(avp.OriginRealm, avp.Mbit, 0, s.OriginRealm)
	a.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Enumerated(1))
	if _, err := a.WriteTo(c); err != nil {
		// diamtest server shutdown races the write; ignore.
		_ = err
	}
}

func dialClient(t *testing.T, addr string) *Client {
	t.Helper()
	cli := &Client{}
	_, err := cli.Connect(ConnectionOptions{
		Addr:            addr,
		Host:            "client.test",
		Realm:           "test.realm",
		NetworkType:     "tcp",
		VendorId:        testVendor3GPP,
		ProductName:     "xk6-diameter-test",
		HostIPAddresses: []string{"127.0.0.1"},
		AppId:           diam.TGPP_S6A_APP_ID,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(cli.Close)
	// Wait briefly for CER/CEA to complete so peer metadata is ready.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cli.Conn != nil && cli.Conn.Context() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cli
}

func airOpts() ConnectionOptions {
	return ConnectionOptions{CompletionSleep: 3}
}

func TestE2E_CheckSendAIR_ReturnsResultCode(t *testing.T) {
	hss := newTestHSS(t, 0)
	t.Cleanup(hss.Close)
	cli := dialClient(t, hss.Addr)

	code, err := cli.CheckSendAIR(airOpts())
	if err != nil {
		t.Fatalf("CheckSendAIR: %v", err)
	}
	if code != int64(diam.Success) {
		t.Fatalf("code = %d; want %d", code, diam.Success)
	}
}

func TestE2E_CheckSendULR_ReturnsResultCode(t *testing.T) {
	hss := newTestHSS(t, 0)
	t.Cleanup(hss.Close)
	cli := dialClient(t, hss.Addr)

	code, err := cli.CheckSendULR(airOpts())
	if err != nil {
		t.Fatalf("CheckSendULR: %v", err)
	}
	if code != int64(diam.Success) {
		t.Fatalf("code = %d; want %d", code, diam.Success)
	}
}

// TestE2E_ConcurrentAIR_HBHCorrelation verifies that many concurrent
// CheckSendAIR calls on the same Client each receive an Answer — the
// property that broke before the HBH-ID correlation refactor.
func TestE2E_ConcurrentAIR_HBHCorrelation(t *testing.T) {
	hss := newTestHSS(t, 50*time.Millisecond) // small delay to overlap requests
	t.Cleanup(hss.Close)
	cli := dialClient(t, hss.Addr)

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			code, err := cli.CheckSendAIR(airOpts())
			if err != nil {
				errs <- err
				return
			}
			if code != int64(diam.Success) {
				errs <- errUnexpectedCode(code)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent AIR error: %v", err)
	}
}

func TestE2E_CheckSendAIR_Timeout(t *testing.T) {
	// Server sleeps longer than the client's CompletionSleep budget.
	hss := newTestHSS(t, 2*time.Second)
	t.Cleanup(hss.Close)
	cli := dialClient(t, hss.Addr)

	opts := ConnectionOptions{CompletionSleep: 1}
	_, err := cli.CheckSendAIR(opts)
	if err == nil {
		t.Fatalf("expected timeout error; got nil")
	}
}

func TestE2E_SendRequest_GenericAIR(t *testing.T) {
	hss := newTestHSS(t, 0)
	t.Cleanup(hss.Close)
	cli := dialClient(t, hss.Addr)

	resp, err := cli.CheckSendRequest(Request{
		AppID: diam.TGPP_S6A_APP_ID,
		Cmd:   diam.AuthenticationInformation,
		AVPs: []AVP{
			{Key: "Session-Id", Value: "sess;1"},
			{Key: "Origin-Host", Value: "client.test"},
			{Key: "Origin-Realm", Value: "test.realm"},
			{Key: "Destination-Realm", Value: "test.realm"},
			{Key: "Auth-Session-State", Value: int64(1)},
			{Key: "User-Name", Value: "001010000000001"},
		},
		CompletionSleep: 3,
	})
	if err != nil {
		t.Fatalf("CheckSendRequest: %v", err)
	}
	if resp == nil || resp.Header.CommandCode != diam.AuthenticationInformation {
		t.Fatalf("unexpected Answer: %+v", resp)
	}
	var rc uint32
	for _, x := range resp.AVP {
		if x.Code == avp.ResultCode {
			rc = uint32(x.Data.(datatype.Unsigned32))
			break
		}
	}
	if rc != diam.Success {
		t.Fatalf("Result-Code = %d; want %d", rc, diam.Success)
	}
}

type errUnexpectedCode int64

func (e errUnexpectedCode) Error() string {
	return "unexpected result code"
}
