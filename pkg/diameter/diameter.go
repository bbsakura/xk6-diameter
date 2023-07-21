package diameter

import (
	"net"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/pkg/errors"
	"go.k6.io/k6/js/modules"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
)

const version = "v0.0.1"

type (
	// RootModule is the global module instance that will create module
	// instances for each VU.
	RootModule struct{}

	// ModuleInstance represents an instance of the GRPC module for every VU.
	ModuleInstance struct {
		Version string
		vu      modules.VU
		exports map[string]interface{}
	}
)

var (
	_ modules.Module   = &RootModule{}
	_ modules.Instance = &ModuleInstance{}
)

// NewModuleInstance implements the modules.Module interface to return
// a new instance for each VU.
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	mi := &ModuleInstance{
		Version: version,
		vu:      vu,
		exports: make(map[string]interface{}),
	}
	mi.exports["K6DiameterClient"] = mi.NewK6DiameterClient
	return mi
}

// Exports implements the modules.Instance interface and returns the exports
// of the JS module.
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{
		Named: mi.exports,
	}
}

type ConnectionOptions struct {
	Addr            string
	Host            string
	Realm           string
	NetworkType     string
	Retries         uint
	VendorID        uint
	AppID           uint
	UeIMSI          string
	PlmnID          string
	Vectors         uint
	CompletionSleep uint
}

type K6DiameterClient struct {
	vu       modules.VU
	Conn     diam.Conn
	sessions *sync.Map
}

func (c *ModuleInstance) NewK6DiameterClient(call goja.ConstructorCall) *goja.Object {
	rt := c.vu.Runtime()
	cli := &K6DiameterClient{
		vu:       c.vu,
		sessions: &sync.Map{},
	}
	return rt.ToValue(cli).ToObject(rt)
}

func (c *K6DiameterClient) Connect(options ConnectionOptions) (bool, error) {
	//var err error
	if len(options.Addr) == 0 {
		return false, errors.New("missing addr")
	}
	cfg := &sm.Settings{
		OriginHost:       datatype.DiameterIdentity(options.Host),
		OriginRealm:      datatype.DiameterIdentity(options.Realm),
		VendorID:         datatype.Unsigned32(options.VendorID),
		ProductName:      "xk6-diameter",
		OriginStateID:    datatype.Unsigned32(time.Now().Unix()),
		FirmwareRevision: 1,
		HostIPAddresses: []datatype.Address{
			datatype.Address(net.ParseIP("127.0.0.1")),
		},
	}
	mux := sm.New(cfg)

	cli := &sm.Client{
		Dict:             dict.Default,
		Handler:          mux,
		MaxRetransmits:   options.Retries,
		EnableWatchdog:   false,
		WatchdogInterval: 0,
		SupportedVendorID: []*diam.AVP{
			diam.NewAVP(avp.SupportedVendorID, avp.Mbit, 0, datatype.Unsigned32(options.VendorID)),
		},
		VendorSpecificApplicationID: []*diam.AVP{
			diam.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(options.AppID)),
					diam.NewAVP(avp.VendorID, avp.Mbit, 0, datatype.Unsigned32(options.VendorID)),
				},
			}),
		},
	}

	conn, err := cli.DialNetwork(options.NetworkType, options.Addr)
	if err != nil {
		return false, errors.WithMessage(err, "Dial error")
	}
	c.Conn = conn
	return true, nil
}

func (c *K6DiameterClient) Close() {
	if c.Conn == nil {
		return
	}
	c.Conn.Close()
}
