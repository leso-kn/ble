package dev

import (
	"github.com/leso-kn/ble"
	"github.com/leso-kn/ble/darwin"
)

// DefaultDevice ...
func DefaultDevice(opts ...ble.Option) (d ble.Device, err error) {
	return darwin.NewDevice(opts...)
}
