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
		}
		length := binary.BigEndian.Uint32(buffer[34:])
		copy(meta.Name[:], buffer[2:34])
		if int64(len(buffer[HeaderLen+MetaDataLen:])) < int64(length) {
			return Header{}, ErrPayloadTooShort
		}
		meta.Payload = buffer[HeaderLen+MetaDataLen:]
		return Header{TypeData, meta}, nil
	default:
		return Header{}, ErrInvalidType
	}
}

func isValidDataType(datatype DataType) bool {
	return datatype >= dataTypeFirst && datatype <= dataTypeLast
}

func (h *Header) Marshal() []byte {

	if h.Type == TypeControl {
		return []byte{TypeControl}
	} else if h.Type == TypeData {
		if h.Data == nil {
			return []byte{TypeData}
		}
		if !isValidDataType(h.Data.(MetaData).Type) {
			return []byte{TypeData}
		}
		meta := h.Data.(MetaData)
		if meta.Payload == nil ||
			len(meta.Payload) == 0 {
			ret := make([]byte, HeaderLen+MetaDataLen)
			ret[0] = TypeData
			ret[1] = uint8(meta.Type)
			copy(ret[2:], meta.Name[:])
			return ret[:]
		} else {
			ret := make([]byte, HeaderLen+MetaDataLen+len(meta.Payload))
			ret[0] = TypeData
			ret[1] = uint8(meta.Type)
			copy(ret[2:], meta.Name[:])
			binary.BigEndian.PutUint32(ret[34:], uint32(len(meta.Payload)))
			copy(ret[HeaderLen+MetaDataLen:], meta.Payload[:len(meta.Payload)])
			return ret
		}
	}
	return nil
}
