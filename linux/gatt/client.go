package gatt

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/leso-kn/ble"
	"github.com/leso-kn/ble/linux/att"
)

const (
	cccNotify   = uint16(0x0001)
	cccIndicate = uint16(0x0002)
)

// A Client is a GATT Client.
type Client struct {
	sync.RWMutex

	profile *ble.Profile
	name    string
	subs    map[uint16]*sub

	ac *att.Client

	conn  ble.Conn
	cache ble.GattCache

	ble.Logger
}

type sub struct {
	cccdh    uint16
	ccc      uint16
	nHandler ble.NotificationHandler
	iHandler ble.NotificationHandler
	id       uint
}

// NewClient returns a GATT Client.
func NewClient(conn ble.Conn, cache ble.GattCache, done chan bool, l ble.Logger) (*Client, error) {
	cl := l.ChildLogger(map[string]interface{}{"gatt": hex.EncodeToString(conn.RemoteAddr().Bytes())})
	p := &Client{
		subs:   make(map[uint16]*sub),
		conn:   conn,
		cache:  cache,
		Logger: cl,
	}
	p.ac = att.NewClient(conn, p, done, cl)

	go p.ac.Loop()

	return p, nil
}

func ClientWithServer(c *Client, db *att.DB) *Client {
	c.ac = c.ac.WithServer(db)
	return c
}

// Addr returns the address of the client.
func (p *Client) Addr() ble.Addr {
	p.RLock()
	defer p.RUnlock()
	return p.conn.RemoteAddr()
}

// Name returns the name of the client.
func (p *Client) Name() string {
	p.RLock()
	defer p.RUnlock()
	return p.name
}

// Profile returns the discovered profile.
func (p *Client) Profile() *ble.Profile {
	p.RLock()
	defer p.RUnlock()
	return p.profile
}

// DiscoverProfile discovers the whole hierarchy of a server.
func (p *Client) DiscoverProfile(force bool) (*ble.Profile, error) {
	if p.profile != nil && !force {
		return p.profile, nil
	}
	ss, err := p.DiscoverServices(nil)
	if err != nil {
		return nil, fmt.Errorf("can't discover services: %s", err)
	}
	for _, s := range ss {
		cs, err := p.DiscoverCharacteristics(nil, s)
		if err != nil {
			return nil, fmt.Errorf("can't discover characteristics: %s", err)
		}
		for _, c := range cs {
			_, err := p.DiscoverDescriptors(nil, c)
			if err != nil {
				return nil, fmt.Errorf("can't discover descriptors: %s", err)
			}
		}
	}
	p.profile = &ble.Profile{Services: ss}
	return p.profile, nil
}

func (p *Client) DiscoverAndCacheProfile(force bool) (*ble.Profile, error) {
	if !force {
		//check cache to see if we have the profile already
		if p.cache != nil {
			profile, err := p.cache.Load(p.Addr())
			if err == nil {
				p.profile = &profile
				return &profile, nil
			}
		}
	}

	profile, err := p.DiscoverProfile(force)
	if err != nil {
		return nil, err
	}

	err = p.cache.Store(p.Addr(), *profile, true)
	if err != nil {
		return profile, err
	}

	return profile, nil
}

// DiscoverServices finds all the primary services on a server. [Vol 3, Part G, 4.4.1]
// If filter is specified, only filtered services are returned.
func (p *Client) DiscoverServices(filter []ble.UUID) ([]*ble.Service, error) {
	p.Lock()
	defer p.Unlock()
	if p.profile == nil {
		p.profile = &ble.Profile{}
	}
	start := uint16(0x0001)
	for {
		length, b, err := p.ac.ReadByGroupType(start, 0xFFFF, ble.PrimaryServiceUUID)
		if err == ble.ErrAttrNotFound {
			return p.profile.Services, nil
		}
		if err != nil {
			return nil, err
		}
		for len(b) != 0 {
			h := binary.LittleEndian.Uint16(b[:2])
			endh := binary.LittleEndian.Uint16(b[2:4])
			u := ble.UUID(b[4:length])
			if filter == nil || ble.Contains(filter, u) {
				s := &ble.Service{
					UUID:      u,
					Handle:    h,
					EndHandle: endh,
				}
				p.profile.Services = append(p.profile.Services, s)
			}
			if endh == 0xFFFF {
				return p.profile.Services, nil
			}

			if len(p.profile.Services) == len(filter) {
				return p.profile.Services, nil
			}
			start = endh + 1
			b = b[length:]
		}
	}
}

// DiscoverIncludedServices finds the included services of a service. [Vol 3, Part G, 4.5.1]
// If filter is specified, only filtered services are returned.
func (p *Client) DiscoverIncludedServices(ss []ble.UUID, s *ble.Service) ([]*ble.Service, error) {
	p.Lock()
	defer p.Unlock()
	return nil, nil
}

// DiscoverCharacteristics finds all the characteristics within a service. [Vol 3, Part G, 4.6.1]
// If filter is specified, only filtered characteristics are returned.
func (p *Client) DiscoverCharacteristics(filter []ble.UUID, s *ble.Service) ([]*ble.Characteristic, error) {
	p.Lock()
	defer p.Unlock()
	start := s.Handle
	var lastChar *ble.Characteristic
	for start <= s.EndHandle {
		length, b, err := p.ac.ReadByType(start, s.EndHandle, ble.CharacteristicUUID)
		if err == ble.ErrAttrNotFound {
			break
		} else if err != nil {
			return nil, err
		}
		for len(b) != 0 {
			h := binary.LittleEndian.Uint16(b[:2])
			p := ble.Property(b[2])
			vh := binary.LittleEndian.Uint16(b[3:5])
			u := ble.UUID(b[5:length])
			c := &ble.Characteristic{
				UUID:        u,
				Property:    p,
				Handle:      h,
				ValueHandle: vh,
				EndHandle:   s.EndHandle,
			}
			if filter == nil || ble.Contains(filter, u) {
				s.Characteristics = append(s.Characteristics, c)
			}
			if lastChar != nil {
				lastChar.EndHandle = c.Handle - 1
			}
			lastChar = c
			start = vh + 1
			b = b[length:]
		}
	}
	return s.Characteristics, nil
}

// DiscoverDescriptors finds all the descriptors within a characteristic. [Vol 3, Part G, 4.7.1]
// If filter is specified, only filtered descriptors are returned.
func (p *Client) DiscoverDescriptors(filter []ble.UUID, c *ble.Characteristic) ([]*ble.Descriptor, error) {
	p.Lock()
	defer p.Unlock()
	start := c.ValueHandle + 1
	for start <= c.EndHandle {
		fmt, b, err := p.ac.FindInformation(start, c.EndHandle)
		if err == ble.ErrAttrNotFound {
			break
		} else if err != nil {
			return nil, err
		}
		length := 2 + 2
		if fmt == 0x02 {
			length = 2 + 16
		}
		for len(b) != 0 {
			h := binary.LittleEndian.Uint16(b[:2])
			u := ble.UUID(b[2:length])
			d := &ble.Descriptor{UUID: u, Handle: h}
			if filter == nil || ble.Contains(filter, u) {
				c.Descriptors = append(c.Descriptors, d)
			}
			if u.Equal(ble.ClientCharacteristicConfigUUID) {
				c.CCCD = d
			}
			start = h + 1
			b = b[length:]
		}
	}
	return c.Descriptors, nil
}

// ReadCharacteristic reads a characteristic value from a server. [Vol 3, Part G, 4.8.1]
func (p *Client) ReadCharacteristic(c *ble.Characteristic) ([]byte, error) {
	p.Lock()
	defer p.Unlock()
	val, err := p.ac.Read(c.ValueHandle)
	if err != nil {
		return nil, err
	}

	c.Value = val
	return val, nil
}

// ReadLongCharacteristic reads a characteristic value which is longer than the MTU. [Vol 3, Part G, 4.8.3]
func (p *Client) ReadLongCharacteristic(c *ble.Characteristic) ([]byte, error) {
	p.Lock()
	defer p.Unlock()

	// The maximum length of an attribute value shall be 512 octects [Vol 3, 3.2.9]
	buffer := make([]byte, 0, 512)

	read, err := p.ac.Read(c.ValueHandle)
	if err != nil {
		return nil, err
	}
	buffer = append(buffer, read...)

	for len(read) >= p.conn.TxMTU()-1 {
		if read, err = p.ac.ReadBlob(c.ValueHandle, uint16(len(buffer))); err != nil {
			return nil, err
		}
		buffer = append(buffer, read...)
	}

	c.Value = buffer
	return buffer, nil
}

// WriteCharacteristic writes a characteristic value to a server. [Vol 3, Part G, 4.9.3]
func (p *Client) WriteCharacteristic(c *ble.Characteristic, v []byte, noRsp bool) error {
	p.Lock()
	defer p.Unlock()
	if noRsp {
		return p.ac.WriteCommand(c.ValueHandle, v)
	}
	return p.ac.Write(c.ValueHandle, v)
}

// ReadDescriptor reads a characteristic descriptor from a server. [Vol 3, Part G, 4.12.1]
func (p *Client) ReadDescriptor(d *ble.Descriptor) ([]byte, error) {
	p.Lock()
	defer p.Unlock()
	val, err := p.ac.Read(d.Handle)
	if err != nil {
		return nil, err
	}

	d.Value = val
	return val, nil
}

// WriteDescriptor writes a characteristic descriptor to a server. [Vol 3, Part G, 4.12.3]
func (p *Client) WriteDescriptor(d *ble.Descriptor, v []byte) error {
	p.Lock()
	defer p.Unlock()
	return p.ac.Write(d.Handle, v)
}

// ReadRSSI retrieves the current RSSI value of remote peripheral. [Vol 2, Part E, 7.5.4]
func (p *Client) ReadRSSI() (int8, error) {
	p.Lock()
	defer p.Unlock()

	return p.ac.ReadRSSI()
}

// ExchangeMTU informs the server of the client’s maximum receive MTU size and
// request the server to respond with its maximum receive MTU size. [Vol 3, Part F, 3.4.2.1]
func (p *Client) ExchangeMTU(mtu int) (int, error) {
	p.Lock()
	defer p.Unlock()
	return p.ac.ExchangeMTU(mtu)
}

// Subscribe subscribes to indication (if ind is set true), or notification of a
// characteristic value. [Vol 3, Part G, 4.10 & 4.11]
func (p *Client) Subscribe(c *ble.Characteristic, ind bool, h ble.NotificationHandler) error {
	p.Lock()
	defer p.Unlock()
	if c.CCCD == nil {
		return fmt.Errorf("CCCD not found")
	}
	flag := cccNotify
	if ind {
		flag = cccIndicate
	}

	return p.setHandlers(c.CCCD.Handle, c.ValueHandle, flag, h)
}

// Unsubscribe unsubscribes to indication (if ind is set true), or notification
// of a specified characteristic value. [Vol 3, Part G, 4.10 & 4.11]
func (p *Client) Unsubscribe(c *ble.Characteristic, ind bool) error {
	p.Lock()
	defer p.Unlock()
	if c.CCCD == nil {
		return fmt.Errorf("CCCD not found")
	}
	if ind {
		return p.setHandlers(c.CCCD.Handle, c.ValueHandle, cccIndicate, nil)
	}
	return p.setHandlers(c.CCCD.Handle, c.ValueHandle, cccNotify, nil)
}

func (p *Client) setHandlers(cccdh, vh, flag uint16, h ble.NotificationHandler) error {
	s, ok := p.subs[vh]
	if !ok {
		s = &sub{cccdh: cccdh}
		p.subs[vh] = s
	}
	switch {
	case h == nil && (s.ccc&flag) == 0:
		return nil
	case h != nil && (s.ccc&flag) != 0:
		return nil
	case h == nil && (s.ccc&flag) != 0:
		s.ccc &= ^uint16(flag)
	case h != nil && (s.ccc&flag) == 0:
		s.ccc |= flag
	}

	v := make([]byte, 2)
	binary.LittleEndian.PutUint16(v, s.ccc)
	if flag == cccNotify {
		s.nHandler = h
	} else {
		s.iHandler = h
	}

	err := p.ac.Write(s.cccdh, v)
	if err != nil {
		delete(p.subs, vh)
	}
	return err
}

// ClearSubscriptions clears all subscriptions to notifications and indications.
func (p *Client) ClearSubscriptions() error {
	p.Lock()
	defer p.Unlock()
	zero := make([]byte, 2)
	for vh, s := range p.subs {
		if err := p.ac.Write(s.cccdh, zero); err != nil {
			return err
		}
		delete(p.subs, vh)
	}
	return nil
}

// CancelConnection disconnects the connection.
func (p *Client) CancelConnection() error {
	p.Lock()
	defer p.Unlock()
	return p.conn.Close()
}

// Disconnected returns a receiving channel, which is closed when the client disconnects.
func (p *Client) Disconnected() <-chan struct{} {
	p.Lock()
	defer p.Unlock()
	return p.conn.Disconnected()
}

// Conn returns the client's current connection.
func (p *Client) Conn() ble.Conn {
	return p.conn
}

// HandleNotification ...
func (p *Client) HandleNotification(req []byte) {
	p.Lock()
	defer p.Unlock()
	vh := att.HandleValueIndication(req).AttributeHandle()
	sub, ok := p.subs[vh]
	if !ok {
		// FIXME: disconnects and propagate an error to the user.
		p.Warnf("got an unregistered notification")
		return
	}

	indication := req[0] == att.HandleValueIndicationCode
	nd := req[3:]

	switch {
	case indication && sub.iHandler != nil:
		sub.iHandler(sub.id, nd)
	case sub.nHandler != nil:
		sub.nHandler(sub.id, nd)
	default:
		select {
		case <-p.conn.Disconnected():
			//ok
		default:
			p.Warnf("no handler, dropping data vh 0x%x, indication %v, id %v, %x", vh, indication, sub.id, nd)
		}
	}
	sub.id++
}

func (p *Client) Pair(authData ble.AuthData, to time.Duration) error {
	return p.conn.Pair(authData, to)
}

func (p *Client) StartEncryption(ch chan ble.EncryptionChangedInfo) error {
	return p.conn.StartEncryption(ch)
}

func (p *Client) PrepareCustomPairing(ch chan bool) {
	p.conn.PrepareCustomPairing(ch)
}
