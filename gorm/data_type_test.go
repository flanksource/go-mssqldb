package sqlserver

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	govalues "github.com/govalues/decimal"
	mssql "github.com/microsoft/go-mssqldb"
	"gorm.io/gorm/schema"
)

type nativeTypesModel struct {
	ID          uuid.UUID `gorm:"primaryKey;default:newsequentialid()"`
	Parent      uuid.NullUUID
	ParentPtr   *uuid.UUID
	Legacy      mssql.UniqueIdentifier
	LegacyNull  mssql.NullUniqueIdentifier
	Tagged      string `gorm:"type:uuid"`
	Amount      govalues.Decimal
	AmountNull  govalues.NullDecimal
	Rate        govalues.Decimal `gorm:"precision:18;scale:6"`
	Whole       govalues.Decimal `gorm:"precision:12"`
	Ratio       float64          `gorm:"precision:10;scale:4"`
	Measurement float64
}

func TestDataTypeOfNativeTypes(t *testing.T) {
	s, err := schema.Parse(&nativeTypesModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}

	dialector := Dialector{Config: &Config{}}
	want := map[string]string{
		"ID":          "uniqueidentifier",
		"Parent":      "uniqueidentifier",
		"ParentPtr":   "uniqueidentifier",
		"Legacy":      "uniqueidentifier",
		"LegacyNull":  "uniqueidentifier",
		"Tagged":      "uniqueidentifier",
		"Amount":      "decimal(38,19)",
		"AmountNull":  "decimal(38,19)",
		"Rate":        "decimal(18,6)",
		"Whole":       "decimal(12)",
		"Ratio":       "decimal(10,4)",
		"Measurement": "float",
	}
	got := map[string]string{}
	for name := range want {
		got[name] = dialector.DataTypeOf(s.LookUpField(name))
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("DataTypeOf(%s) = %q, want %q", name, got[name], w)
		}
	}
}
