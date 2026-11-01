package sqlserver

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type uuidKeyed struct {
	ID     uuid.UUID `gorm:"primaryKey;default:newsequentialid()"`
	Parent uuid.UUID
	Name   string
}

func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	pool, err := sql.Open("sqlserver", "sqlserver://localhost?database=dryrun")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(New(Config{Conn: pool}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateOutputsDatabaseGeneratedUUIDKey(t *testing.T) {
	parent := uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")
	tx := dryRunDB(t).Create(&uuidKeyed{Parent: parent, Name: "acme"})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	stmt := tx.Statement

	sql := stmt.SQL.String()
	if want := `INSERT INTO "uuid_keyeds" ("parent","name") OUTPUT INSERTED."id" VALUES (@p1,@p2);`; sql != want {
		t.Errorf("Create SQL = %s, want %s", sql, want)
	}
	if got, ok := stmt.Vars[0].(uuid.UUID); !ok || got != parent {
		t.Errorf("Create bound %#v for parent, want the uuid.UUID %s (sent as uniqueidentifier)", stmt.Vars[0], parent)
	}
}

func TestExplainRendersUUIDAsLiteral(t *testing.T) {
	id := uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")
	got := Dialector{Config: &Config{}}.Explain(`SELECT * FROM "t" WHERE "id" = @p1`, id)

	if want := `SELECT * FROM "t" WHERE "id" = '` + id.String() + `'`; !strings.EqualFold(got, want) {
		t.Errorf("Explain = %s, want %s", got, want)
	}
}
