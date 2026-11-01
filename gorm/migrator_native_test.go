package sqlserver

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	govalues "github.com/govalues/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
)

type nativeColumns struct {
	Parent uuid.UUID
	Amount govalues.Decimal
	Rate   govalues.Decimal `gorm:"precision:18;scale:6"`
	Ratio  float64          `gorm:"precision:10;scale:4"`
}

// liveColumn is the column as Migrator.ColumnTypes reads it back:
// INFORMATION_SCHEMA's DATA_TYPE and NUMERIC_PRECISION/SCALE, and the
// driver's upper-case DatabaseTypeName.
func liveColumn(name, dataType string, precision, scale int64) migrator.ColumnType {
	column := migrator.ColumnType{
		NameValue:       sql.NullString{String: name, Valid: true},
		ColumnTypeValue: sql.NullString{String: dataType, Valid: true},
		DataTypeValue:   sql.NullString{String: dataType, Valid: true},
		NullableValue:   sql.NullBool{Bool: true, Valid: true},
		UniqueValue:     sql.NullBool{Valid: true},
	}
	if precision > 0 {
		column.DecimalSizeValue = sql.NullInt64{Int64: precision, Valid: true}
		column.ScaleValue = sql.NullInt64{Int64: scale, Valid: true}
	}
	return column
}

func TestMigrateColumnKeepsUnchangedNativeColumns(t *testing.T) {
	db := dryRunDB(t)
	var executed []string
	if err := db.Callback().Raw().After("gorm:raw").Register("test:record", func(tx *gorm.DB) {
		executed = append(executed, tx.Statement.SQL.String())
	}); err != nil {
		t.Fatal(err)
	}

	m := db.Migrator().(Migrator)
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&nativeColumns{}); err != nil {
		t.Fatal(err)
	}

	for field, live := range map[string]migrator.ColumnType{
		"Parent": liveColumn("parent", "UNIQUEIDENTIFIER", 0, 0),
		"Amount": liveColumn("amount", "DECIMAL", 38, 19),
		"Rate":   liveColumn("rate", "DECIMAL", 18, 6),
		"Ratio":  liveColumn("ratio", "DECIMAL", 10, 4),
	} {
		if err := m.Migrator.MigrateColumn(&nativeColumns{}, stmt.Schema.LookUpField(field), live); err != nil {
			t.Fatalf("MigrateColumn(%s): %v", field, err)
		}
	}

	if len(executed) > 0 {
		t.Errorf("MigrateColumn altered unchanged columns: %q", executed)
	}
}
