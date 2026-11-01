package mssql

import (
	"database/sql/driver"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
	govalues "github.com/govalues/decimal"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// Native types (msdsn.EncodeParameters.LegacyTypes unset, `native types=true`):
// a govalues decimal is bound as decimal(38, s) with s its own scale, as the
// JDBC driver binds a BigDecimal; a money value as money; a uuid.UUID as
// uniqueidentifier; and a uniqueidentifier is read as its canonical string.
// `native types=false` keeps the upstream driver's text bindings.

// nativeParam is val bound with its native type, ok false for a value with no
// native binding or when the connection keeps the legacy text bindings.
func (s *Stmt) nativeParam(val driver.Value) (res param, ok bool, err error) {
	switch val.(type) {
	case govalues.Decimal, govalues.NullDecimal,
		Money[govalues.Decimal], Money[govalues.NullDecimal], uuid.UUID, uuid.NullUUID:
	default:
		return res, false, nil
	}
	encoding := s.c.sess.encoding
	if encoding.LegacyTypes {
		return res, false, nil
	}
	switch v := val.(type) {
	case govalues.Decimal:
		return makeDecimalParam(v), true, nil
	case govalues.NullDecimal:
		if v.Valid {
			return makeDecimalParam(v.Decimal), true, nil
		}
		return nullDecimalParam(), true, nil
	case Money[govalues.Decimal]:
		res, err = makeMoneyParam(v.Decimal)
		return res, true, err
	case Money[govalues.NullDecimal]:
		if v.Decimal.Valid {
			res, err = makeMoneyParam(v.Decimal.Decimal)
			return res, true, err
		}
		res.ti.TypeId = typeMoneyN
		res.ti.Size = 8
		res.buffer = []byte{}
		return res, true, nil
	case uuid.UUID:
		return makeUUIDParam(v, encoding), true, nil
	case uuid.NullUUID:
		if v.Valid {
			return makeUUIDParam(v.UUID, encoding), true, nil
		}
		res.ti.TypeId = typeGuid
		res.ti.Size = 16
		res.buffer = []byte{}
		return res, true, nil
	}
	panic(fmt.Sprintf("mssql: nativeParam has no native binding for %T", val))
}

// decimalParamPrecision is the precision a decimal parameter is declared with:
// SQL Server's largest, so the declaration depends only on the value's scale.
const decimalParamPrecision = 38

// decimalParamSize is a DECIMAL(38) value on the wire: a sign byte, then the
// 16-byte little-endian magnitude.
const decimalParamSize = 17

// makeDecimalParam binds a govalues decimal, which holds at most 19 digits, so
// every one fits DECIMAL(38).
func makeDecimalParam(val govalues.Decimal) param {
	res := nullDecimalParam()
	res.ti.Scale = uint8(val.Scale())
	res.buffer = make([]byte, decimalParamSize)
	if !val.IsNeg() {
		res.buffer[0] = 1
	}
	binary.LittleEndian.PutUint64(res.buffer[1:], val.Coef())
	return res
}

// nullDecimalParam is a NULL decimal parameter, declared decimal(38, 0).
func nullDecimalParam() (res param) {
	res.ti.TypeId = typeDecimalN
	res.ti.Prec = decimalParamPrecision
	res.ti.Size = decimalParamSize
	res.buffer = []byte{}
	return res
}

func makeUUIDParam(val uuid.UUID, encoding msdsn.EncodeParameters) (res param) {
	res.ti.TypeId = typeGuid
	res.ti.Size = 16
	res.buffer = guidBuffer(val, encoding)
	return res
}

// guidBuffer is u as writeGuidType expects its buffer: SQL Server sends a
// uniqueidentifier's first three groups little-endian, and writeGuidType
// swaps them itself under `guid conversion`, so the buffer is in RFC 4122
// order then and swapped here otherwise.
func guidBuffer(u uuid.UUID, encoding msdsn.EncodeParameters) []byte {
	buf := make([]byte, 16)
	copy(buf, u[:])
	if !encoding.GuidConversion {
		swapGUIDGroups(buf)
	}
	return buf
}

// swapGUIDGroups converts a uniqueidentifier between RFC 4122 and SQL Server's
// wire order: its first three groups reversed, in place.
func swapGUIDGroups(b []byte) {
	reverse := func(s []byte) {
		for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
			s[i], s[j] = s[j], s[i]
		}
	}
	reverse(b[0:4])
	reverse(b[4:6])
	reverse(b[6:8])
}
