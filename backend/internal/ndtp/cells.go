package ndtp

import (
	"encoding/binary"
	"errors"
	"time"
)

const (
	CellTypeNav00 uint8 = 0
	CellNav00Size int   = 26
)

var (
	ErrInvalidCellType = errors.New("unexpected cell type")
	ErrCellTooShort    = errors.New("payload too short for cell")
)

// NavRecord is the clean domain telemetry event extracted from G6CellNav00.
type NavRecord struct {
	UnitID     uint32    `json:"unit_id"`
	Timestamp  time.Time `json:"timestamp"`
	Latitude   float64   `json:"lat"`
	Longitude  float64   `json:"lon"`
	SpeedAvg   uint16    `json:"speed_avg"` // km/h
	SpeedMax   uint16    `json:"speed_max"` // km/h
	Course     uint16    `json:"course"`
	Altitude   uint16    `json:"altitude"`
	Valid      bool      `json:"valid"`
	Satellites uint8     `json:"satellites"`
}

// ParseNav00Cell parses type 0 G6CellNav00.
// Structure: [type: u8][number: u8][payload: 26 bytes]
func ParseNav00Cell(unitID uint32, data []byte) (*NavRecord, error) {
	if len(data) < 2+CellNav00Size {
		return nil, ErrCellTooShort
	}

	cellType := data[0]
	if cellType != CellTypeNav00 {
		return nil, ErrInvalidCellType
	}

	payload := data[2 : 2+CellNav00Size]

	// 1. Timestamp (Unix seconds)
	sec := binary.LittleEndian.Uint32(payload[0:4])
	tm := time.Unix(int64(sec), 0).UTC()

	// 2. Coordinates
	lonRaw := binary.LittleEndian.Uint32(payload[4:8])
	latRaw := binary.LittleEndian.Uint32(payload[8:12])
	extraBits := payload[12]

	isNorth := (extraBits & (1 << 5)) != 0
	isEast := (extraBits & (1 << 6)) != 0
	isValid := (extraBits & (1 << 7)) != 0

	lat := float64(latRaw) / 10000000.0
	if !isNorth {
		lat = -lat
	}

	lon := float64(lonRaw) / 10000000.0
	if !isEast {
		lon = -lon
	}

	// 3. Motion parameters
	speedAvg := binary.LittleEndian.Uint16(payload[14:16])
	speedMax := binary.LittleEndian.Uint16(payload[16:18])
	course := binary.LittleEndian.Uint16(payload[18:20])
	altitude := binary.LittleEndian.Uint16(payload[22:24])
	nsat := payload[24]

	return &NavRecord{
		UnitID:     unitID,
		Timestamp:  tm,
		Latitude:   lat,
		Longitude:  lon,
		SpeedAvg:   speedAvg,
		SpeedMax:   speedMax,
		Course:     course,
		Altitude:   altitude,
		Valid:      isValid,
		Satellites: nsat,
	}, nil
}
