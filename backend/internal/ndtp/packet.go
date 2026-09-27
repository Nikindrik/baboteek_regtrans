package ndtp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	NPLSignature  uint16 = 0x7E7E
	NPLHeaderSize int    = 15
	NPHHeaderSize int    = 10

	// NPH Services
	ServiceGenericControls uint16 = 0
	ServiceNavdata         uint16 = 1

	// NPH Types
	TypeConnRequest uint16 = 100 // Handshake
	TypeRealtime    uint16 = 101 // Telemetry data
)

var (
	ErrInvalidSignature = errors.New("invalid NPL signature")
	ErrCRCMismatch      = errors.New("NPH packet CRC mismatch")
	ErrPacketTooShort   = errors.New("packet is shorter than expected size")
)

// NPLHeader represents Navigation Packet Layer (15 bytes).
type NPLHeader struct {
	Signature   uint16
	DataSize    uint16 // Size of NPH header + Body
	Flags       uint16
	CRC         uint16 // Byte-swapped CRC-16/Modbus
	Type        uint8  // 0x02 = NPH
	PeerAddress uint32 // unitId
	RequestID   uint16
}

// NPHHeader represents Navigation Protocol Handler (10 bytes).
type NPHHeader struct {
	ServiceID uint16
	Type      uint16
	Flags     uint16
	RequestID uint32
}

func ParseNPLHeader(buf []byte) (*NPLHeader, error) {
	if len(buf) < NPLHeaderSize {
		return nil, ErrPacketTooShort
	}

	sig := binary.LittleEndian.Uint16(buf[0:2])
	if sig != NPLSignature {
		return nil, fmt.Errorf("%w: got 0x%04X, expected 0x%04X", ErrInvalidSignature, sig, NPLSignature)
	}

	return &NPLHeader{
		Signature:   sig,
		DataSize:    binary.LittleEndian.Uint16(buf[2:4]),
		Flags:       binary.LittleEndian.Uint16(buf[4:6]),
		CRC:         binary.LittleEndian.Uint16(buf[6:8]),
		Type:        buf[8],
		PeerAddress: binary.LittleEndian.Uint32(buf[9:13]),
		RequestID:   binary.LittleEndian.Uint16(buf[13:15]),
	}, nil
}

func ParseNPHHeader(buf []byte) (*NPHHeader, error) {
	if len(buf) < NPHHeaderSize {
		return nil, ErrPacketTooShort
	}

	return &NPHHeader{
		ServiceID: binary.LittleEndian.Uint16(buf[0:2]),
		Type:      binary.LittleEndian.Uint16(buf[2:4]),
		Flags:     binary.LittleEndian.Uint16(buf[4:6]),
		RequestID: binary.LittleEndian.Uint32(buf[6:10]),
	}, nil
}
