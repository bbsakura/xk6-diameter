package diameter

import (
	"errors"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

type AVP struct {
	// TODO: json.UnmarshalJSON() to accept `[{"User-Name": "000000000"}]`
	Key   string
	Value interface{}
}

type AvpMeta struct {
	code      uint32
	flag      uint8
	vendor    uint32
	converter func(interface{}) (datatype.Type, error)
}

func (pair *AVP) modifyMessage(m *diam.Message, meta *smpeer.Metadata) error {
	avpMeta, ok := avpDict[pair.Key]
	if !ok {
		return errors.New("not found AVP")
	}
	val, err := avpMeta.converter(pair.Value)
	if err != nil {
		return err
	}
	_, err = m.NewAVP(pair.Key, avpMeta.flag, avpMeta.vendor, val)
	if err != nil {
		return err
	}
	return nil
}

func appendAVPs(m *diam.Message, meta *smpeer.Metadata, avps []AVP) error {
	var errs []error
	for _, pair := range avps {
		err := pair.modifyMessage(m, meta)
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func modifyMessage(m *diam.Message, meta *smpeer.Metadata, options ConnectionOptions) error {
	if options.DestinationHost == nil {
		_, err := m.NewAVP(avp.DestinationHost, avp.Mbit, 0, meta.OriginHost)
		if err != nil {
			return err
		}
	} else if *options.DestinationHost != "" {
		_, err := m.NewAVP(avp.DestinationHost, avp.Mbit, 0, *options.DestinationHost)
		if err != nil {
			return err
		}
	}
	if options.DestinationRealm == nil {
		_, err := m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, meta.OriginRealm)
		if err != nil {
			return err
		}
	} else {
		_, err := m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, *options.DestinationRealm)
		if err != nil {
			return err
		}
	}
	return nil
}
