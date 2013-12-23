package mssql

import (
    "io"
    "encoding/binary"
    "math"
    "time"
)


// fixed-length data types
// http://msdn.microsoft.com/en-us/library/dd341171.aspx
const (
    typeNull = 0x1f
    typeInt1 = 0x30
    typeBit = 0x32
    typeInt2 = 0x34
    typeInt4 = 0x38
    typeDateTim4 = 0x3a
    typeFlt4 = 0x3b
    typeMoney = 0x3c
    typeDateTime = 0x3d
    typeFlt8 = 0x3e
    typeMoney4 = 0x7a
    typeInt8 = 0x7f
)

// variable-length data types
// http://msdn.microsoft.com/en-us/library/dd358341.aspx
const (
    // byte len types
    typeGuid = 0x24
    typeIntN = 0x26
    typeDecimal = 0x37  // legacy
    typeNumeric = 0x3f  // legacy
    typeBitN = 0x68
    typeDecimalN = 0x6a
    typeNumericN = 0x6c
    typeFltN = 0x6d
    typeMoneyN = 0x6e
    typeDateTimeN = 0x6f
    typeDateN = 0x28
    typeTimeN = 0x29
    typeDateTime2N = 0x2a
    typeDateTimeOffsetN = 0x2b
    typeChar = 0x2f // legacy
    typeVarChar = 0x27 // legacy
    typeBinary = 0x2d // legacy
    typeVarBinary = 0x25 // legacy

    // short length types
    typeBigVarBin = 0xa5
    typeBigVarChar = 0xa7
    typeBigBinary = 0xad
    typeBigChar = 0xaf
    typeNVarChar = 0xe7
    typeNChar = 0xef
    typeXml = 0xf1
    typeUdt = 0xf0

    // long length types
    typeText = 0x23
    typeImage = 0x22
    typeNText = 0x63
    typeVariant = 0x62
)


// http://msdn.microsoft.com/en-us/library/ee780895.aspx
func decodeDateTim4(buf []byte) time.Time {
    days := binary.LittleEndian.Uint16(buf)
    mins := binary.LittleEndian.Uint16(buf[2:])
    return time.Date(1900, 1, 1 + int(days),
                     0, int(mins), 0, 0, time.UTC)
}

func decodeDateTime(buf []byte) time.Time {
    days := int32(binary.LittleEndian.Uint32(buf))
    tm := binary.LittleEndian.Uint32(buf[4:])
    ns := int(math.Trunc(float64(tm % 300 * 10000000) / 3.0))
    secs := int(tm / 300)
    return time.Date(1900, 1, 1 + int(days),
                     0, 0, secs, ns, time.UTC)
}


func readFixedType(column *columnStruct, r io.Reader) (res []byte, err error) {
    _, err = io.ReadFull(r, column.Buffer)
    return column.Buffer, nil
}

func readByteLenType(column *columnStruct, r io.Reader) (res []byte, err error) {
    var size uint8
    err = binary.Read(r, binary.LittleEndian, &size); if err != nil {
        return
    }
    if size == 0 {
        return nil, nil
    }
    _, err = io.ReadFull(r, column.Buffer[:size]); if err != nil {
        return
    }
    return column.Buffer[:size], nil
}

func readShortLenType(column *columnStruct, r io.Reader) (res []byte, err error) {
    var size uint16
    err = binary.Read(r, binary.LittleEndian, &size); if err != nil {
        return
    }
    if size == 0 {
        return nil, nil
    }
    _, err = io.ReadFull(r, column.Buffer[:size]); if err != nil {
        return
    }
    return column.Buffer[:size], nil
}

func readLongLenType(column *columnStruct, r io.Reader) (res []byte, err error) {
    panic("Not implemented")
}

func readVarLen(column *columnStruct, r io.Reader) (err error) {
    switch column.TypeId {
    case typeDateN:
        column.Size = 3
        column.Reader = readByteLenType
        column.Buffer = make([]byte, column.Size)
    case typeTimeN, typeDateTime2N, typeDateTimeOffsetN:
        err = binary.Read(r, binary.LittleEndian, &column.Scale); if err != nil {
            return
        }
        switch column.Scale {
        case 1, 2:
            column.Size = 3
        case 3, 4:
            column.Size = 4
        case 5, 6, 7:
            column.Size = 5
        default:
            err = streamErrorf("Invalid scale for TIME/DATETIME2/DATETIMEOFFSET type")
            return
        }
        switch column.TypeId {
        case typeDateTime2N:
            column.Size += 3
        case typeDateTimeOffsetN:
            column.Size += 5
        }
        column.Reader = readByteLenType
        column.Buffer = make([]byte, column.Size)
    case typeGuid, typeIntN, typeDecimal, typeNumeric,
            typeBitN, typeDecimalN, typeNumericN, typeFltN,
            typeMoneyN, typeDateTimeN, typeChar,
            typeVarChar, typeBinary, typeVarBinary:
        // byle len types
        var bytesize uint8
        err = binary.Read(r, binary.LittleEndian, &bytesize); if err != nil {
            return
        }
        column.Size = int(bytesize)
        column.Buffer = make([]byte, column.Size)
        switch column.TypeId {
        case typeDecimal, typeNumeric, typeDecimalN, typeNumericN:
            err = binary.Read(r, binary.LittleEndian, &column.Prec); if err != nil {
                return
            }
            err = binary.Read(r, binary.LittleEndian, &column.Scale); if err != nil {
                return
            }
        }
        column.Reader = readByteLenType
    case typeBigVarBin, typeBigVarChar, typeBigBinary, typeBigChar,
            typeNVarChar, typeNChar, typeXml, typeUdt:
        // short len types
        var ushortsize uint16
        err = binary.Read(r, binary.LittleEndian, &ushortsize); if err != nil {
            return
        }
        column.Size = int(ushortsize)
        switch column.TypeId {
        case typeBigVarChar, typeBigChar, typeNVarChar, typeNChar:
            column.Collation, err = readCollation(r); if err != nil {
                return
            }
        case typeXml:
            panic("XMLTYPE not implemented")
        }
        if column.Size == 0xffff {
            panic("PARTLENTYPE not yet supported")
        } else {
            column.Buffer = make([]byte, column.Size)
            column.Reader = readShortLenType
        }
    case typeText, typeImage, typeNText, typeVariant:
        // LONGLEN_TYPE
        var longsize int32
        err = binary.Read(r, binary.LittleEndian, &longsize); if err != nil {
            return
        }
        switch column.TypeId {
        case typeText, typeNText:
            column.Collation, err = readCollation(r); if err != nil {
                return
            }
        case typeXml:
            panic("XMLTYPE not implemented")
        }
        column.Size = int(longsize)
        column.Reader = readLongLenType
    default:
        return streamErrorf("Invalid type %d", column.TypeId)
    }
    return
}


func decodeMoney(buf []byte) int {
    panic("Not implemented")
}

func decodeMoney4(buf []byte) int {
    panic("Not implemented")
}

func decodeGuid(buf []byte) (res [16]byte) {
    copy(res[:], buf)
    return
}

func decodeDecimal(column columnStruct, buf []byte) Decimal {
    var sign uint8
    sign = buf[0]
    dec := Decimal{
        positive: sign != 0,
        prec: column.Prec,
        scale: column.Scale,
    }
    buf = buf[1:]
    for i := 0; i < len(buf) / 4; i++ {
        dec.integer[i] = binary.LittleEndian.Uint32(buf)
        buf = buf[4:]
    }
    return dec
}

// http://msdn.microsoft.com/en-us/library/ee780895.aspx
func decodeDateInt(buf []byte) (days int) {
    return int(buf[0]) + int(buf[1]) * 256 + int(buf[2]) * 256 * 256
}

func decodeDate(buf []byte) time.Time {
    return time.Date(1, 1, 1 + decodeDateInt(buf), 0, 0, 0, 0, time.UTC)
}

func decodeTimeInt(scale uint8, buf []byte) (sec int, ns int) {
    var acc uint64 = 0
    for i := len(buf) - 1; i >= 0; i-- {
        acc <<= 8
        acc |= uint64(buf[i])
    }
    for i := 0; i < 7 - int(scale); i++ {
        acc *= 10
    }
    nsbig := acc * 100
    sec = int(nsbig / 1000000000)
    ns = int(nsbig % 1000000000)
    return
}

func decodeTime(column columnStruct, buf []byte) time.Time {
    sec, ns := decodeTimeInt(column.Scale, buf)
    return time.Date(1, 1, 1, 0, 0, sec, ns, time.UTC)
}

func decodeDateTime2(scale uint8, buf []byte) time.Time {
    timesize := len(buf) - 3
    sec, ns := decodeTimeInt(scale, buf[:timesize])
    days := decodeDateInt(buf[timesize:])
    return time.Date(1, 1, 1 + days, 0, 0, sec, ns, time.UTC)
}

func decodeDateTimeOffset(buf []byte) int {
    panic("Not implemented")
}

func decodeChar(column columnStruct, buf []byte) string {
    return string(buf)
}

func decodeNChar(column columnStruct, buf []byte) (string, error) {
    return ucs22utf8.ConvertString(string(buf))
}

func decodeXml(column columnStruct, buf []byte) int {
    panic("Not implemented")
}

func decodeUdt(column columnStruct, buf []byte) int {
    panic("Not implemented")
}
