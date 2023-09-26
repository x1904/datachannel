package protocol

import (
	"encoding/binary"
	"errors"
)

type Message []byte

type Type uint8

const (
	TypeControl = iota + 1
	TypeData
)

type DataType uint8

const (
	DataTypeJSON = iota + 1
	DataTypeBIN32
	DataTypeBIN64
	DataTypeRAW
	dataTypeFirst = DataTypeJSON
	dataTypeLast  = DataTypeRAW
)

const MetaDataLen = 1 + 32 + 4

type MetaData struct {
	Type    DataType
	Name    [32]byte
	Len     uint32
	Payload []byte
}

const HeaderLen = 1

var ErrBufferNull = errors.New("buffer is null")
var ErrBufferTooShort = errors.New("buffer too short")
var ErrNotImplemented = errors.New("not implemented")
var ErrInvalidType = errors.New("type invalid")
var ErrInvalidDataType = errors.New("datatype invalid")
var ErrPayloadTooShort = errors.New("payload too short")

type Header struct {
	Type Type
	Data interface{}
}

func Unmarshal(buffer []byte) (Header, error) {
	if buffer == nil {
		return Header{}, ErrBufferNull
	}

	if len(buffer) < HeaderLen {
		return Header{}, ErrBufferTooShort
	}

	switch Type(buffer[0]) {
	case TypeControl:
		return Header{}, ErrNotImplemented
	case TypeData:
		if len(buffer[1:]) < MetaDataLen {
			return Header{}, ErrBufferTooShort
		}
		if !isValidDataType(DataType(buffer[1])) {
			return Header{}, ErrInvalidDataType
		}
		meta := MetaData{
			Type: DataType(buffer[1]),
			Len:  binary.BigEndian.Uint32(buffer[34:]),
		}
		copy(meta.Name[:], buffer[2:34])
		if int64(len(buffer[MetaDataLen:])) < int64(meta.Len) {
			return Header{}, ErrPayloadTooShort
		}
		meta.Payload = buffer[MetaDataLen:]
		return Header{TypeData, meta}, nil
	default:
		return Header{}, ErrInvalidType
	}
}

func isValidDataType(datatype DataType) bool {
	return datatype >= dataTypeFirst && datatype <= dataTypeLast
}
