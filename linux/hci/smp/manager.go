package smp

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/leso-kn/ble"
	"github.com/leso-kn/ble/linux/hci"
	"github.com/leso-kn/ble/sliceops"
)

type PairingState int

const (
	Init PairingState = iota
	WaitPairingResponse
	WaitPublicKey
	WaitConfirm
	WaitRandom
	WaitDhKeyCheck
	Finished
	Error
)

type manager struct {
	config      hci.SmpConfig
	pairing     *pairingContext
	t           *transport
	bondManager hci.BondManager
	encrypt     func(info hci.BondInfo) error
	result      chan error
	ble.Logger
}

//todo: need to have on instance per connection which requires a mutex in the bond manager
//todo: remove bond manager from input parameters?
func NewSmpManager(config hci.SmpConfig, bm hci.BondManager, l ble.Logger) *manager {
	p := &pairingContext{request: config, state: Init, Logger: l}
	m := &manager{config: config, pairing: p, bondManager: bm, result: make(chan error), Logger: l}
	t := NewSmpTransport(p, bm, m, nil, nil, l)
	m.t = t
	return m
}

func (m *manager) SetConfig(config hci.SmpConfig) {
	m.config = config
}

func (m *manager) SetWritePDUFunc(w func([]byte) (int, error)) {
	m.t.writePDU = w
}

func (m *manager) SetEncryptFunc(e func(info hci.BondInfo) error) {
	m.encrypt = e
}

func (m *manager) SetNOPFunc(f func() error) {
	m.t.nopFunc = f
}

func (m *manager) InitContext(localAddr, remoteAddr []byte,
	localAddrType, remoteAddrType uint8) {
	if m.pairing == nil {
		m.pairing = &pairingContext{}
	}

	m.pairing.localAddr = sliceops.SwapBuf(localAddr)
	m.pairing.localAddrType = localAddrType
	m.pairing.remoteAddr = sliceops.SwapBuf(remoteAddr)
	m.pairing.remoteAddrType = remoteAddrType

	m.t.pairing = m.pairing
}

func (m *manager) Handle(in []byte) error {
	p := pdu(in)
	payload := p.payload()
	code := payload[0]
	data := payload[1:]
	v, ok := dispatcher[code]
	if !ok || v.handler == nil {
		m.Errorf("smp: unhandled smp code %v", code)

		// C.5.1 Pairing Not Supported
		return m.t.send([]byte{pairingFailed, 0x05})
	}

	_, err := v.handler(m.t, data)
	if err != nil {
		m.t.pairing.state = Error
		m.result <- err
		return err
	}

	if m.t.pairing.state == Finished {
		select {
		case <-m.result:
		default:
			close(m.result)
		}
	}

	return nil
}

func (m *manager) Pair(authData ble.AuthData, to time.Duration) error {
	if m.t.pairing.state != Init {
		return fmt.Errorf("Pairing already in progress")
	}

	//todo: can this be made less bad??
	m.t.pairing = m.pairing
	m.t.pairing.authData = authData

	//set a default timeout
	if to <= time.Duration(0) {
		to = time.Minute
	}

	if len(authData.OOBData) > 0 {
		m.t.pairing.request.OobFlag = byte(hci.OobPreset)
	}

	err := m.t.StartPairing(to)
	if err != nil {
		return err
	}

	return m.waitResult(to)
}

func (m *manager) PrepareCustomPairing(ch chan bool) {
	m.pairing.customPairingHandler = &ch
}

func (m *manager) waitResult(to time.Duration) error {
	select {
	case err := <-m.result:
		return err
	case <-time.After(to):
		return fmt.Errorf("pairing operation timed out")
	}
}

func (m *manager) StartEncryption() error {
	bi, err := m.bondManager.Find(hex.EncodeToString(m.pairing.remoteAddr))
	if err != nil {
		return err
	}
	return m.encrypt(bi)
}

//todo: implement if needed
func (m *manager) BondInfoFor(addr string) hci.BondInfo {
	bi, err := m.bondManager.Find(addr)
	if err != nil {
		m.Errorf("bondInfoFor: %v", err)
		return nil
	}

	return bi
}

func (m *manager) DeleteBondInfo() error {
	return m.bondManager.Delete(hex.EncodeToString(m.pairing.remoteAddr))
}

func (m *manager) SaveBondInfo(bi hci.BondInfo) error {
	return m.bondManager.Save(hex.EncodeToString(m.pairing.remoteAddr), bi)
}

func (m *manager) LegacyPairingInfo() (bool, []byte) {
	if m.pairing.legacy {
		return true, m.pairing.shortTermKey
	}

	return false, nil
}

func (m *manager) EnableEncryption(addr string) error {
	return m.encrypt(m.pairing.bond)
}

func (m *manager) Encrypt() error {
	return m.encrypt(m.pairing.bond)
}
