package hci

import (
	"fmt"
	"io"
	"time"

	"github.com/leso-kn/ble/linux/hci/h4"
	"github.com/leso-kn/ble/linux/hci/socket"
)

type transportHci struct {
	id int
}

type transportH4Socket struct {
	addr    string
	timeout time.Duration
}

type transportH4Uart struct {
	path string
	baud int
}

type transport struct {
	hci      *transportHci
	h4uart   *transportH4Uart
	h4socket *transportH4Socket
}

func getTransport(t transport) (io.ReadWriteCloser, error) {
	switch {
	case t.hci != nil:
		return socket.NewSocket(t.hci.id)

	case t.h4socket != nil:
		return h4.NewSocket(t.h4socket.addr, t.h4socket.timeout)

	case t.h4uart != nil:
		so := h4.DefaultSerialOptions()
		so.PortName = t.h4uart.path
		if t.h4uart.baud != -1 {
			so.BaudRate = uint(t.h4uart.baud)
		}
		return h4.NewSerial(so)

	default:
		return nil, fmt.Errorf("no valid transport found")
	}
}
