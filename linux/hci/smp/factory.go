package smp

import (
	"github.com/leso-kn/ble"
	"github.com/leso-kn/ble/linux/hci"
)

type factory struct {
	bm hci.BondManager
}

func NewSmpFactory(bm hci.BondManager) *factory {
	return &factory{bm}
}

func (f *factory) Create(config hci.SmpConfig, l ble.Logger) hci.SmpManager {
	return NewSmpManager(config, f.bm, l)
}

func (f *factory) SetBondManager(bm hci.BondManager) {
	f.bm = bm
}
