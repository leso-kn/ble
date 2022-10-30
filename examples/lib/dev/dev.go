package dev

import (
	"github.com/leso-kn/ble"
)

// NewDevice ...
func NewDevice(impl string, opts ...ble.Option) (d ble.Device, err error) {
	return DefaultDevice(opts...)
}
