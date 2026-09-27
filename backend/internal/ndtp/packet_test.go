package ndtp

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestParseNav00MatchesOfficialLayout(t *testing.T) {
	cell := make([]byte, 2+CellNav00Size)
	cell[0] = CellTypeNav00
	cell[1] = 0
	p := cell[2:]
	binary.LittleEndian.PutUint32(p[0:4], 1767672000)
	binary.LittleEndian.PutUint32(p[4:8], 376173210)
	binary.LittleEndian.PutUint32(p[8:12], 557551234)
	p[12] = (1 << 5) | (1 << 6) | (1 << 7) // N, E, valid
	binary.LittleEndian.PutUint16(p[14:16], 42)
	binary.LittleEndian.PutUint16(p[16:18], 51)
	binary.LittleEndian.PutUint16(p[18:20], 123)
	binary.LittleEndian.PutUint16(p[20:22], 777)
	binary.LittleEndian.PutUint16(p[22:24], 156)
	p[24] = 11
	p[25] = 2

	got, err := ParseNav00Cell(1166336, cell)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnitID != 1166336 || !got.Valid || got.SpeedAvg != 42 || got.SpeedMax != 51 || got.Course != 123 || got.Altitude != 156 || got.Satellites != 11 {
		t.Fatalf("unexpected decoded values: %+v", got)
	}
	if math.Abs(got.Latitude-55.7551234) > 1e-7 || math.Abs(got.Longitude-37.6173210) > 1e-7 {
		t.Fatalf("unexpected coordinates: lat=%v lon=%v", got.Latitude, got.Longitude)
	}
	if !got.Timestamp.Equal(time.Unix(1767672000, 0).UTC()) {
		t.Fatalf("unexpected timestamp: %v", got.Timestamp)
	}
}

func TestNPLNPHAndSwappedModbusCRC(t *testing.T) {
	nph := make([]byte, NPHHeaderSize)
	binary.LittleEndian.PutUint16(nph[0:2], ServiceNavdata)
	binary.LittleEndian.PutUint16(nph[2:4], TypeRealtime)
	binary.LittleEndian.PutUint16(nph[4:6], 1)
	binary.LittleEndian.PutUint32(nph[6:10], 12345)

	rawCRC := ModbusCRC16(nph)
	swapped := (rawCRC >> 8) | (rawCRC << 8)
	npl := make([]byte, NPLHeaderSize)
	binary.LittleEndian.PutUint16(npl[0:2], NPLSignature)
	binary.LittleEndian.PutUint16(npl[2:4], uint16(len(nph)))
	binary.LittleEndian.PutUint16(npl[6:8], swapped)
	npl[8] = 0x02
	binary.LittleEndian.PutUint32(npl[9:13], 1166336)

	h, err := ParseNPLHeader(npl)
	if err != nil {
		t.Fatal(err)
	}
	if h.PeerAddress != 1166336 || h.DataSize != uint16(len(nph)) || !CheckCRC(nph, h.CRC) {
		t.Fatalf("bad NPL decode/CRC: %+v", h)
	}
	nh, err := ParseNPHHeader(nph)
	if err != nil {
		t.Fatal(err)
	}
	if nh.ServiceID != ServiceNavdata || nh.Type != TypeRealtime || nh.RequestID != 12345 {
		t.Fatalf("bad NPH decode: %+v", nh)
	}
}
