package dev

import (
	"github.com/leso-kn/ble"
	"github.com/leso-kn/ble/linux"
)

// DefaultDevice ...
func DefaultDevice(opts ...ble.Option) (d ble.Device, err error) {
	return linux.NewDevice(opts...)
}
