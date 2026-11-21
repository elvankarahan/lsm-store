package sstable

import (
	"encoding/binary"
)

const (
	FooterLen = 40
	Magic     = uint64(0xdb4c534d7374626c) // "dbLSMstbl"
)

type Footer struct {
	FilterOffset uint64
	FilterLength uint64
	IndexOffset  uint64
	IndexLength  uint64
}

func (f Footer) Encode(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, f.FilterOffset)
	dst = binary.LittleEndian.AppendUint64(dst, f.FilterLength)
	dst = binary.LittleEndian.AppendUint64(dst, f.IndexOffset)
	dst = binary.LittleEndian.AppendUint64(dst, f.IndexLength)
	dst = binary.LittleEndian.AppendUint64(dst, Magic)
	return dst
}
func DecodeFooter(data []byte) (Footer, error) {
	if len(data) < FooterLen {
		return Footer{}, ErrCorrupt
	}

	data = data[len(data)-FooterLen:]

	magic := binary.LittleEndian.Uint64(data[32:40])
	if magic != Magic {
		return Footer{}, ErrCorrupt
	}

	return Footer{
		FilterOffset: binary.LittleEndian.Uint64(data[0:8]),
		FilterLength: binary.LittleEndian.Uint64(data[8:16]),
		IndexOffset:  binary.LittleEndian.Uint64(data[16:24]),
		IndexLength:  binary.LittleEndian.Uint64(data[24:32]),
	}, nil

}
