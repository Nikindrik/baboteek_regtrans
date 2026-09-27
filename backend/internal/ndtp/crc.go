package ndtp

// ModbusCRC16 calculates the CRC-16/Modbus checksum (poly 0xA001, init 0xFFFF).
func ModbusCRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc & 0x0001) != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// CheckCRC validates if calculated CRC with swapped bytes matches packet CRC.
func CheckCRC(data []byte, expectedCRC uint16) bool {
	rawCRC := ModbusCRC16(data)
	// Swap bytes according to NDTP spec
	swappedCRC := (rawCRC >> 8) | (rawCRC << 8)
	return swappedCRC == expectedCRC
}
