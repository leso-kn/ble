package att

import (
	"encoding/binary"
	"errors"

	"fmt"
	"io"
	"time"

	"github.com/leso-kn/ble"
)

// NotificationHandler handles notification or indication.
type NotificationHandler interface {
	HandleNotification(req []byte)
}

// Client implementation an Attribute Protocol Client.
type Client struct {
	l2c  ble.Conn
	rspc chan []byte
	inc  chan []byte

	rxBuf      []byte
	chTxBuf    chan []byte
	chErr      chan error
	handler    NotificationHandler
	done       chan bool
	connClosed chan struct{}

	server *Server
	ble.Logger
}

// NewClient returns an Attribute Protocol Client.
func NewClient(l2c ble.Conn, h NotificationHandler, done chan bool, l ble.Logger) *Client {
	c := &Client{
		l2c:        l2c,
		rspc:       make(chan []byte),
		inc:        make(chan []byte, 10),
		chTxBuf:    make(chan []byte, 1),
		rxBuf:      make([]byte, ble.MaxMTU),
		chErr:      make(chan error, 1),
		handler:    h,
		done:       done,
		connClosed: make(chan struct{}),
		Logger:     l,
	}
	c.chTxBuf <- make([]byte, l2c.TxMTU())

	go func() {
		<-l2c.Disconnected()
		close(c.connClosed)
	}()

	return c
}

func (c *Client) WithServer(db *DB) *Client {
	var err error
	c.server, err = NewServer(db, c.l2c, c.Logger)
	if err != nil {
		c.Errorf("failed to create server")
	}

	return c
}

// ExchangeMTU informs the server of the client’s maximum receive MTU size and
// request the server to respond with its maximum receive MTU size. [Vol 3, Part F, 3.4.2.1]
func (c *Client) ExchangeMTU(clientRxMTU int) (serverRxMTU int, err error) {
	if clientRxMTU < ble.DefaultMTU || clientRxMTU > ble.MaxMTU {
		return 0, ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	// The same txBuf, or a newly allocate one, if the txMTU is changed,
	// will be released back to the channel.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	// Let L2CAP know the MTU we can handle.
	c.l2c.SetRxMTU(clientRxMTU)

	req := ExchangeMTURequest(txBuf[:3])
	req.SetAttributeOpcode()
	req.SetClientRxMTU(uint16(clientRxMTU))

	b, err := c.sendReq(req)
	if err != nil {
		return 0, err
	}

	// Convert and validate the response.
	rsp := ExchangeMTUResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return 0, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) != 3:
		return 0, ErrInvalidResponse
	}

	txMTU := int(rsp.ServerRxMTU())
	if len(txBuf) != txMTU {
		// Let L2CAP know the MTU that the remote device can handle.
		c.l2c.SetTxMTU(txMTU)
		// Put a re-allocated txBuf back to the channel.
		// The txBuf has been captured in deferred function.
		txBuf = make([]byte, txMTU)
	}

	return txMTU, nil
}

func (c *Client) ReadRSSI() (int8, error) {
	return c.l2c.ReadRSSI()
}

// FindInformation obtains the mapping of attribute handles with their associated types.
// This allows a Client to discover the list of attributes and their types on a server.
// [Vol 3, Part F, 3.4.3.1 & 3.4.3.2]
func (c *Client) FindInformation(starth, endh uint16) (fmt int, data []byte, err error) {
	if starth == 0 || starth > endh {
		return 0x00, nil, ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := FindInformationRequest(txBuf[:5])
	req.SetAttributeOpcode()
	req.SetStartingHandle(starth)
	req.SetEndingHandle(endh)

	b, err := c.sendReq(req)
	if err != nil {
		return 0x00, nil, err
	}

	// Convert and validate the response.
	rsp := FindInformationResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return 0x00, nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 6:
		fallthrough
	case rsp.Format() == 0x01 && ((len(rsp)-2)%4) != 0:
		fallthrough
	case rsp.Format() == 0x02 && ((len(rsp)-2)%18) != 0:
		return 0x00, nil, ErrInvalidResponse
	}
	return int(rsp.Format()), rsp.InformationData(), nil
}

// // HandleInformationList ...
// type HandleInformationList []byte
//
// // FoundAttributeHandle ...
// func (l HandleInformationList) FoundAttributeHandle() []byte { return l[:2] }
//
// // GroupEndHandle ...
// func (l HandleInformationList) GroupEndHandle() []byte { return l[2:4] }
//
// // FindByTypeValue ...
// func (c *Client) FindByTypeValue(starth, endh, attrType uint16, value []byte) ([]HandleInformationList, error) {
// 	return nil, nil
// }

// ReadByType obtains the values of attributes where the attribute type is known
// but the handle is not known. [Vol 3, Part F, 3.4.4.1 & 3.4.4.2]
func (c *Client) ReadByType(starth, endh uint16, uuid ble.UUID) (int, []byte, error) {
	if starth > endh || (len(uuid) != 2 && len(uuid) != 16) {
		return 0, nil, ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := ReadByTypeRequest(txBuf[:5+len(uuid)])
	req.SetAttributeOpcode()
	req.SetStartingHandle(starth)
	req.SetEndingHandle(endh)
	req.SetAttributeType(uuid)

	b, err := c.sendReq(req)
	if err != nil {
		return 0, nil, err
	}

	// Convert and validate the response.
	rsp := ReadByTypeResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return 0, nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 4 || len(rsp.AttributeDataList())%int(rsp.Length()) != 0:
		return 0, nil, ErrInvalidResponse
	}
	return int(rsp.Length()), rsp.AttributeDataList(), nil
}

// Read requests the server to read the value of an attribute and return its
// value in a Read Response. [Vol 3, Part F, 3.4.4.3 & 3.4.4.4]
func (c *Client) Read(handle uint16) ([]byte, error) {

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := ReadRequest(txBuf[:3])
	req.SetAttributeOpcode()
	req.SetAttributeHandle(handle)

	b, err := c.sendReq(req)
	if err != nil {
		return nil, err
	}

	// Convert and validate the response.
	rsp := ReadResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 1:
		return nil, ErrInvalidResponse
	}
	return rsp.AttributeValue(), nil
}

// ReadBlob requests the server to read part of the value of an attribute at a
// given offset and return a specific part of the value in a Read Blob Response.
// [Vol 3, Part F, 3.4.4.5 & 3.4.4.6]
func (c *Client) ReadBlob(handle, offset uint16) ([]byte, error) {

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := ReadBlobRequest(txBuf[:5])
	req.SetAttributeOpcode()
	req.SetAttributeHandle(handle)
	req.SetValueOffset(offset)

	b, err := c.sendReq(req)
	if err != nil {
		return nil, err
	}

	// Convert and validate the response.
	rsp := ReadBlobResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 1:
		return nil, ErrInvalidResponse
	}
	return rsp.PartAttributeValue(), nil
}

// ReadMultiple requests the server to read two or more values of a set of
// attributes and return their values in a Read Multiple Response.
// Only values that have a known fixed size can be read, with the exception of
// the last value that can have a variable length. The knowledge of whether
// attributes have a known fixed size is defined in a higher layer specification.
// [Vol 3, Part F, 3.4.4.7 & 3.4.4.8]
func (c *Client) ReadMultiple(handles []uint16) ([]byte, error) {
	// Should request to read two or more values.
	if len(handles) < 2 || len(handles)*2 > c.l2c.TxMTU()-1 {
		return nil, ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := ReadMultipleRequest(txBuf[:1+len(handles)*2])
	req.SetAttributeOpcode()
	p := req.SetOfHandles()
	for _, h := range handles {
		binary.LittleEndian.PutUint16(p, h)
		p = p[2:]
	}

	b, err := c.sendReq(req)
	if err != nil {
		return nil, err
	}

	// Convert and validate the response.
	rsp := ReadMultipleResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 1:
		return nil, ErrInvalidResponse
	}
	return rsp.SetOfValues(), nil
}

// ReadByGroupType obtains the values of attributes where the attribute type is known,
// the type of a grouping attribute as defined by a higher layer specification, but
// the handle is not known. [Vol 3, Part F, 3.4.4.9 & 3.4.4.10]
func (c *Client) ReadByGroupType(starth, endh uint16, uuid ble.UUID) (int, []byte, error) {
	if starth > endh || (len(uuid) != 2 && len(uuid) != 16) {
		return 0, nil, ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := ReadByGroupTypeRequest(txBuf[:5+len(uuid)])
	req.SetAttributeOpcode()
	req.SetStartingHandle(starth)
	req.SetEndingHandle(endh)
	req.SetAttributeGroupType(uuid)

	b, err := c.sendReq(req)
	if err != nil {
		return 0, nil, err
	}

	// Convert and validate the response.
	rsp := ReadByGroupTypeResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return 0, nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 4:
		fallthrough
	case len(rsp.AttributeDataList())%int(rsp.Length()) != 0:
		return 0, nil, ErrInvalidResponse
	}

	return int(rsp.Length()), rsp.AttributeDataList(), nil
}

// Write requests the server to write the value of an attribute and acknowledge that
// this has been achieved in a Write Response. [Vol 3, Part F, 3.4.5.1 & 3.4.5.2]
func (c *Client) Write(handle uint16, value []byte) error {
	if len(value) > c.l2c.TxMTU()-3 {
		return ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := WriteRequest(txBuf[:3+len(value)])
	req.SetAttributeOpcode()
	req.SetAttributeHandle(handle)
	req.SetAttributeValue(value)

	b, err := c.sendReq(req)
	if err != nil {
		return err
	}

	// Convert and validate the response.
	rsp := WriteResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		return ErrInvalidResponse
	}
	return nil
}

// WriteCommand requests the server to write the value of an attribute, typically
// into a control-point attribute. [Vol 3, Part F, 3.4.5.3]
func (c *Client) WriteCommand(handle uint16, value []byte) error {
	if len(value) > c.l2c.TxMTU()-3 {
		return ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := WriteCommand(txBuf[:3+len(value)])
	req.SetAttributeOpcode()
	req.SetAttributeHandle(handle)
	req.SetAttributeValue(value)

	return c.sendCmd(req)
}

// SignedWrite requests the server to write the value of an attribute with an authentication
// signature, typically into a control-point attribute. [Vol 3, Part F, 3.4.5.4]
func (c *Client) SignedWrite(handle uint16, value []byte, signature [12]byte) error {
	if len(value) > c.l2c.TxMTU()-15 {
		return ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := SignedWriteCommand(txBuf[:15+len(value)])
	req.SetAttributeOpcode()
	req.SetAttributeHandle(handle)
	req.SetAttributeValue(value)
	req.SetAuthenticationSignature(signature)

	return c.sendCmd(req)
}

// PrepareWrite requests the server to prepare to write the value of an attribute.
// The server will respond to this request with a Prepare Write Response, so that
// the Client can verify that the value was received correctly.
// [Vol 3, Part F, 3.4.6.1 & 3.4.6.2]
func (c *Client) PrepareWrite(handle uint16, offset uint16, value []byte) (uint16, uint16, []byte, error) {
	if len(value) > c.l2c.TxMTU()-5 {
		return 0, 0, nil, ErrInvalidArgument
	}

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := PrepareWriteRequest(txBuf[:5+len(value)])
	req.SetAttributeOpcode()
	req.SetAttributeHandle(handle)
	req.SetValueOffset(offset)

	b, err := c.sendReq(req)
	if err != nil {
		return 0, 0, nil, err
	}

	// Convert and validate the response.
	rsp := PrepareWriteResponse(b)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return 0, 0, nil, ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) != 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		fallthrough
	case len(rsp) < 5:
		return 0, 0, nil, ErrInvalidResponse
	}
	return rsp.AttributeHandle(), rsp.ValueOffset(), rsp.PartAttributeValue(), nil
}

// ExecuteWrite requests the server to write or cancel the write of all the prepared
// values currently held in the prepare queue from this Client. This request shall be
// handled by the server as an atomic operation. [Vol 3, Part F, 3.4.6.3 & 3.4.6.4]
func (c *Client) ExecuteWrite(flags uint8) error {

	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()

	req := ExecuteWriteRequest(txBuf[:1])
	req.SetAttributeOpcode()
	req.SetFlags(flags)

	rspBytes, err := c.sendReq(req)
	if err != nil {
		return err
	}

	// Convert and validate the response.
	rsp := ExecuteWriteResponse(rspBytes)
	switch {
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		return ble.ATTError(rsp[4])
	case rsp[0] == ErrorResponseCode && len(rsp) == 5:
		fallthrough
	case rsp[0] != rsp.AttributeOpcode():
		return ErrInvalidResponse
	}
	return nil
}

func (c *Client) sendCmd(b []byte) error {
	_, err := c.l2c.Write(b)
	return err
}

func (c *Client) sendReq(b []byte) (rsp []byte, err error) {
	c.Debugf("req: %x", b)
	if _, err := c.l2c.Write(b); err != nil {
		return nil, fmt.Errorf("send ATT request failed: %w", err)
	}
	for {
		select {
		case rsp := <-c.rspc:
			if rsp[0] == ErrorResponseCode || rsp[0] == rspOfReq[b[0]] {
				return rsp, nil
			}
			// Sometimes when we connect to an Apple device, it sends
			// ATT requests asynchronously to us. // In this case, we
			// returns an ErrReqNotSupp response, and continue to wait
			// the response to our request.
			errRsp := newErrorResponse(rsp[0], 0x0000, ble.ErrReqNotSupp)
			c.Debugf("rsp: %x", b)
			_, err := c.l2c.Write(errRsp)
			if err != nil {
				return nil, fmt.Errorf("unexpected ATT response received: %w", err)
			}
		case err := <-c.chErr:
			return nil, fmt.Errorf("ATT request failed: %w", err)
		case <-time.After(2 * time.Second):
			return nil, fmt.Errorf("ATT request timeout: %w", ErrSeqProtoTimeout)
		}
	}

}

func (c *Client) sendResp(rsp []byte) error {
	// Acquire and reuse the txBuf, and release it after usage.
	txBuf := <-c.chTxBuf
	defer func() { c.chTxBuf <- txBuf }()
	if c.l2c == nil {
		return fmt.Errorf("ble conn was nil")
	}
	if _, err := c.l2c.Write(rsp); err != nil {
		return fmt.Errorf("send ATT request failed: %w", err)
	}

	return nil
}

func (c *Client) asyncReqLoop() {
	for {
		// keep trying?
		select {
		case <-c.done:
			c.Debug("exited async loop: done")
			return
		case <-c.connClosed:
			c.Debug("exited async loop: conn closed")
			return
		default:
			if c.l2c == nil {
				c.Debug("exited async loop: l2c nil")
				return
			}
			//ok
		}

		in := <-c.inc
		rsp := c.server.HandleRequest(in)
		if rsp == nil {
			continue
		}
		err := c.sendResp(rsp)
		if err != nil {
			c.Errorf("failed to send async att response for: %X", in[0])
		}
	}
}

// Loop ...
func (c *Client) Loop() {

	type asyncWork struct {
		handle func([]byte)
		data   []byte
	}

	ch := make(chan asyncWork, 16)
	defer close(ch)
	go func() {
		for w := range ch {
			w.handle(w.data)
		}
	}()

	//start up async response handling
	if c.server != nil {
		go c.asyncReqLoop()
		defer func() {
			close(c.inc)
		}()
	}

	confirmation := []byte{HandleValueConfirmationCode}
	for {
		// keep trying?
		select {
		case <-c.done:
			c.Debug("exited async loop: done")
			return
		case <-c.connClosed:
			c.Debug("exited async loop: conn closed")
			return
		default:
			if c.l2c == nil {
				c.Debug("exited async loop: l2c nil")
				return
			}
			//ok
		}

		n, err := c.l2c.Read(c.rxBuf)
		// keep trying?
		select {
		case <-c.done:
			c.Debug("exited async loop: done")
			return
		case <-c.connClosed:
			c.Debug("exited async loop: conn closed")
			return
		default:
			if c.l2c == nil {
				c.Debug("exited async loop: l2c nil")
				return
			} else if err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					c.Debugf("input channel closed while reading due to disconnection or connection failure")
					c.chErr <- fmt.Errorf("disconnected")
				} else {
					c.Errorf("client: read %v", err)

					// We don't expect any error from the bearer (L2CAP ACL-U)
					// Pass it along to the pending request, if any, and escape.
					c.chErr <- err
				}
				return
			}
			//ok
		}

		b := make([]byte, n)
		copy(b, c.rxBuf)
		c.Debugf("rx: %x", b)

		//all incoming requests are even numbered
		//which means the last bit should be 0
		if b[0]&0x01 == 0x00 {
			select {
			case <-c.done:
				c.Info("exited async loop: closed after async req rx")
				return
			case <-c.connClosed:
				c.Debug("exited async loop: conn closed")
				return
			case c.inc <- b:
				continue
			default:
				c.Errorf("failed to enqueue request for %x", b[0])
				continue
			}
		}

		if (b[0] != HandleValueNotificationCode) && (b[0] != HandleValueIndicationCode) {
			c.Debugf("a rx: %x", c.rxBuf[:n])
			select {
			case <-c.done:
				c.Info("exited client loop: closed after rsp rx")
				return
			case <-c.connClosed:
				c.Debug("exited client async loop: conn closed")
				return
			case c.rspc <- b:
				continue
			}
		}

		// Deliver the full request to upper layer.
		c.Debugf("notif: %x", b)
		select {
		case <-c.done:
			c.Info("exited async loop: closed after rx")
			return
		case <-c.connClosed:
			c.Debug("exited async loop: conn closed")
			return
		case ch <- asyncWork{handle: c.handler.HandleNotification, data: b}:
			// ok
		default:
			// If this really happens, especially on a slow machine, enlarge the channel buffer.
			c.Error("can't enqueue incoming notification.")
		}

		// Always write aknowledgement for an indication, even it was an invalid request.
		if b[0] == HandleValueIndicationCode {
			c.Debugf("write confirmation for indication")
			_, _ = c.l2c.Write(confirmation)
		}
	}
}
