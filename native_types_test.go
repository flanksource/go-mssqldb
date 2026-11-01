package mssql

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/google/uuid"
	govalues "github.com/govalues/decimal"
	"github.com/microsoft/go-mssqldb/msdsn"
	shopspring "github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stmtWithEncoding(encoding msdsn.EncodeParameters) *Stmt {
	return &Stmt{c: &Conn{sess: &tdsSession{encoding: encoding}}}
}

// rfcGUID and wireGUID are one uniqueidentifier in RFC 4122 order and as SQL
// Server sends it, its first three groups little-endian.
var (
	rfcGUID  = uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")
	wireGUID = []byte{0x67, 0x45, 0x23, 0x01, 0xab, 0x89, 0xef, 0xcd, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
)

func TestNativeDecimalParam(t *testing.T) {
	t.Parallel()
	s := stmtWithEncoding(msdsn.EncodeParameters{})

	testCases := []struct {
		name  string
		value any
		scale uint8
		// want is the value as SQL Server reads the parameter back.
		want string
	}{
		{name: "govalues with a fractional part", value: govalues.MustParse("2.534"), scale: 3, want: "2.534"},
		{name: "govalues negative", value: govalues.MustParse("-0.00252234"), scale: 8, want: "-0.00252234"},
		{name: "govalues keeps trailing zeros", value: govalues.MustParse("2.500"), scale: 3, want: "2.500"},
		{name: "govalues at its 19 digits", value: govalues.MustParse("-9999999999.999999999"), scale: 9, want: "-9999999999.999999999"},
		{name: "govalues valid NullDecimal", value: govalues.NullDecimal{Decimal: govalues.MustParse("-670000"), Valid: true}, want: "-670000"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.makeParam(tc.value)
			require.NoError(t, err)
			assert.Equal(t, uint8(typeDecimalN), res.ti.TypeId)
			assert.Equal(t, tc.scale, res.ti.Scale)
			assert.Equal(t, 17, res.ti.Size)
			assert.Equal(t, tc.want, string(decodeDecimal(res.ti.Prec, res.ti.Scale, res.buffer)))
		})
	}
}

func TestNativeDecimalParamDeclaration(t *testing.T) {
	t.Parallel()
	res, err := stmtWithEncoding(msdsn.EncodeParameters{}).makeParam(govalues.MustParse("25.34"))
	require.NoError(t, err)
	assert.Equal(t, "decimal(38, 2)", makeDecl(res.ti))
}

func TestNativeNullParams(t *testing.T) {
	t.Parallel()
	s := stmtWithEncoding(msdsn.EncodeParameters{})

	testCases := []struct {
		name   string
		value  any
		typeID uint8
		size   int
	}{
		{name: "govalues", value: govalues.NullDecimal{}, typeID: typeDecimalN, size: 17},
		{name: "money", value: Money[govalues.NullDecimal]{}, typeID: typeMoneyN, size: 8},
		{name: "uuid", value: uuid.NullUUID{}, typeID: typeGuid, size: 16},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.makeParam(tc.value)
			require.NoError(t, err)
			assert.Equal(t, tc.typeID, res.ti.TypeId)
			assert.Equal(t, tc.size, res.ti.Size)
			assert.Empty(t, res.buffer)
		})
	}
}

// A shopspring decimal has no native binding: it keeps upstream's nvarchar
// text under native types too.
func TestShopspringDecimalStaysText(t *testing.T) {
	t.Parallel()
	s := stmtWithEncoding(msdsn.EncodeParameters{})
	for name, value := range map[string]driver.Valuer{
		"Decimal":     shopspring.New(2534, -3),
		"NullDecimal": shopspring.NewNullDecimal(shopspring.New(2534, -3)),
	} {
		t.Run(name, func(t *testing.T) {
			v, err := value.Value()
			require.NoError(t, err)
			res, err := s.makeParam(v)
			require.NoError(t, err)
			assert.Equal(t, uint8(typeNVarChar), res.ti.TypeId)
			u, _ := ucs22str(res.buffer)
			assert.Equal(t, "2.534", u)
		})
	}
}

func TestNativeMoneyParam(t *testing.T) {
	t.Parallel()
	res, err := stmtWithEncoding(msdsn.EncodeParameters{}).makeParam(Money[govalues.Decimal]{govalues.MustNew(-12345, 2)})
	require.NoError(t, err)
	assert.Equal(t, uint8(typeMoneyN), res.ti.TypeId)
	assert.Equal(t, "-123.4500", string(decodeMoney(res.buffer)))
}

func TestMoneyParamRange(t *testing.T) {
	t.Parallel()
	s := stmtWithEncoding(msdsn.EncodeParameters{})

	for value, want := range map[string]string{
		"922337203685477.5807":  "922337203685477.5807",
		"-922337203685477.5808": "-922337203685477.5808",
		"1.23456":               "1.2345",
		"-1.23456":              "-1.2345",
	} {
		t.Run("binds "+value, func(t *testing.T) {
			res, err := s.makeParam(Money[govalues.Decimal]{govalues.MustParse(value)})
			require.NoError(t, err)
			assert.Equal(t, want, string(decodeMoney(res.buffer)))
		})
	}
	for _, value := range []string{"922337203685477.5808", "-922337203685477.5809", "1000000000000000"} {
		t.Run("refuses "+value, func(t *testing.T) {
			_, err := s.makeParam(Money[govalues.Decimal]{govalues.MustParse(value)})
			assert.ErrorContains(t, err, "outside the money range")
		})
	}
}

func TestNativeUUIDParam(t *testing.T) {
	t.Parallel()

	t.Run("sent in wire order", func(t *testing.T) {
		res, err := stmtWithEncoding(msdsn.EncodeParameters{}).makeParam(rfcGUID)
		require.NoError(t, err)
		assert.Equal(t, uint8(typeGuid), res.ti.TypeId)
		assert.Equal(t, "uniqueidentifier", makeDecl(res.ti))
		assert.Equal(t, wireGUID, res.buffer)
	})

	// Native types read a uniqueidentifier as the GUID the server holds
	// whatever `guid conversion` says, so every GUID parameter must reach the
	// wire as that same GUID too.
	var legacyUID UniqueIdentifier
	require.NoError(t, legacyUID.Scan(rfcGUID.String()))
	for name, value := range map[string]driver.Value{
		"uuid.UUID":            rfcGUID,
		"uuid.NullUUID":        uuid.NullUUID{UUID: rfcGUID, Valid: true},
		"UniqueIdentifier":     legacyUID,
		"NullUniqueIdentifier": NullUniqueIdentifier{UUID: legacyUID, Valid: true},
	} {
		t.Run(name+" ignores guid conversion", func(t *testing.T) {
			encoding := msdsn.EncodeParameters{GuidConversion: true}
			res, err := stmtWithEncoding(encoding).makeParam(value)
			require.NoError(t, err)
			var wire bytesWriter
			require.NoError(t, writeGuidType(&wire, res.ti, res.buffer, encoding))
			assert.Equal(t, append([]byte{16}, wireGUID...), []byte(wire))
		})
	}

	t.Run("legacy guid conversion swaps a UniqueIdentifier's bytes", func(t *testing.T) {
		encoding := msdsn.EncodeParameters{GuidConversion: true, LegacyTypes: true}
		res, err := stmtWithEncoding(encoding).makeParam(legacyUID)
		require.NoError(t, err)
		var wire bytesWriter
		require.NoError(t, writeGuidType(&wire, res.ti, res.buffer, encoding))
		assert.Equal(t, append([]byte{16}, rfcGUID[:]...), []byte(wire))
	})
}

func TestGuidBufferForBulkCopy(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		encoding msdsn.EncodeParameters
		want     []byte
	}{
		"native":                         {msdsn.EncodeParameters{}, wireGUID},
		"native ignores guid conversion": {msdsn.EncodeParameters{GuidConversion: true}, wireGUID},
		"legacy guid conversion leaves RFC order for writeGuidType": {
			msdsn.EncodeParameters{GuidConversion: true, LegacyTypes: true}, rfcGUID[:]},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, guidBuffer(rfcGUID, tc.encoding))
		})
	}
}

// bytesWriter collects what a typeInfo writer writes.
type bytesWriter []byte

func (w *bytesWriter) Write(p []byte) (int, error) {
	*w = append(*w, p...)
	return len(p), nil
}

// native types=false keeps the upstream driver's nvarchar text bindings.
func TestLegacyTypesKeepTextParams(t *testing.T) {
	t.Parallel()
	s := stmtWithEncoding(msdsn.EncodeParameters{LegacyTypes: true})
	for name, value := range map[string]any{
		"govalues":   govalues.MustParse("2.534"),
		"shopspring": shopspring.New(2534, -3),
		"money":      Money[govalues.Decimal]{govalues.MustNew(1, 0)},
		"uuid":       rfcGUID,
	} {
		t.Run(name, func(t *testing.T) {
			res, err := s.makeParam(value)
			require.NoError(t, err)
			assert.Equal(t, uint8(typeNVarChar), res.ti.TypeId)
		})
	}
}

func TestNativeOutParam(t *testing.T) {
	t.Parallel()
	res, err := stmtWithEncoding(msdsn.EncodeParameters{}).makeParamExtra(sql.Out{Dest: govalues.MustParse("1.5")})
	require.NoError(t, err)
	assert.Equal(t, uint8(typeDecimalN), res.ti.TypeId)
	assert.Equal(t, uint8(fByRevValue), res.Flags)
	assert.Equal(t, "1.5", string(decodeDecimal(res.ti.Prec, res.ti.Scale, res.buffer)))
}

func TestDecodeGuidNative(t *testing.T) {
	t.Parallel()
	for name, encoding := range map[string]msdsn.EncodeParameters{
		"native":                         {},
		"native ignores guid conversion": {GuidConversion: true},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "01234567-89ab-cdef-0123-456789abcdef", decodeGuid(wireGUID, encoding))
		})
	}
}

// Every Scanner a uniqueidentifier is read into reads the native string as
// the same GUID it read from the legacy bytes.
func TestNativeGuidScans(t *testing.T) {
	t.Parallel()
	native := decodeGuid(wireGUID, msdsn.EncodeParameters{})

	var u uuid.UUID
	require.NoError(t, u.Scan(native))
	assert.Equal(t, rfcGUID, u)

	var nu uuid.NullUUID
	require.NoError(t, nu.Scan(native))
	assert.Equal(t, uuid.NullUUID{UUID: rfcGUID, Valid: true}, nu)

	var legacy, fromString UniqueIdentifier
	require.NoError(t, legacy.Scan(decodeGuid(wireGUID, msdsn.EncodeParameters{LegacyTypes: true})))
	require.NoError(t, fromString.Scan(native))
	assert.Equal(t, legacy, fromString)
}

func TestBulkGuidFromString(t *testing.T) {
	t.Parallel()
	col := columnStruct{ti: typeInfo{TypeId: typeGuid, Size: 16}}
	for name, encoding := range map[string]msdsn.EncodeParameters{
		"no guid conversion": {},
		"guid conversion":    {GuidConversion: true},
	} {
		t.Run(name, func(t *testing.T) {
			b := &Bulk{cn: &Conn{sess: &tdsSession{encoding: encoding}}}
			res, err := b.makeParam(rfcGUID, col)
			require.NoError(t, err)
			var wire bytesWriter
			require.NoError(t, writeGuidType(&wire, res.ti, res.buffer, encoding))
			assert.Equal(t, append([]byte{16}, wireGUID...), []byte(wire))
		})
	}

	t.Run("not a GUID", func(t *testing.T) {
		b := &Bulk{cn: &Conn{sess: &tdsSession{}}}
		_, err := b.makeParam("not-a-guid", col)
		assert.Error(t, err)
	})
}
