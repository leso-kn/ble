package ble

// DefaultMTU defines the default MTU of ATT protocol including 3 bytes of ATT header.
const DefaultMTU = 339

// MaxMTU is maximum of ATT_MTU, which is 512 bytes of value length, plus 3 bytes of ATT header.
// The maximum length of an attribute value shall be 512 octets [Vol 3, Part F, 3.2.9]
const MaxMTU = 512 + 3

// UUIDs ...
var (
	GAPUUID         = UUID16(0x1800) // Generic Access
	GATTUUID        = UUID16(0x1801) // Generic Attribute
	CurrentTimeUUID = UUID16(0x1805) // Current Time Service
	DeviceInfoUUID  = UUID16(0x180A) // Device Information
	BatteryUUID     = UUID16(0x180F) // Battery Service
	HIDUUID         = UUID16(0x1812) // Human Interface Device

	PrimaryServiceUUID   = UUID16(0x2800)
	SecondaryServiceUUID = UUID16(0x2801)
	IncludeUUID          = UUID16(0x2802)
	CharacteristicUUID   = UUID16(0x2803)

	ClientCharacteristicConfigUUID = UUID16(0x2902)
	ServerCharacteristicConfigUUID = UUID16(0x2903)

	DeviceNameUUID               = UUID16(0x2A00)
	AppearanceUUID               = UUID16(0x2A01)
	PeripheralPrivacyUUID        = UUID16(0x2A02)
	ReconnectionAddrUUID         = UUID16(0x2A03)
	PeferredParamsUUID           = UUID16(0x2A04)
	CentralAddressResolutionUUID = UUID16(0x2AA6)
	ServiceChangedUUID           = UUID16(0x2A05)
	SystemIDUUID                 = UUID16(0x2A23)
	ModelNumberUUID              = UUID16(0x2A24)
	SerialNumberUUID             = UUID16(0x2A25)
	FirmwareRevisionStringUUID   = UUID16(0x2A26)
	HardwareRevisionUUID         = UUID16(0x2A27)
	SoftwareRevisionStringUUID   = UUID16(0x2A28)
	ManufacturerNameUUID         = UUID16(0x2A29)
	PnPIDUUID                    = UUID16(0x2A50)

	IEEE1107320601RegulatoryCertificationDataListUUID = UUID16(0x2A2A)
)
