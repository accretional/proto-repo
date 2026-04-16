// Package schema holds embedded SQLite DDL and DB open helpers.
package schema

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

//go:embed source.sql
var SourceDDL string

//go:embed protos.sql
var ProtosDDL string

// OpenFresh removes any file at path then creates and initializes a new SQLite
// DB with the given DDL applied. Since we always re-index, there is no
// migration path — we just start over.
func OpenFresh(path, ddl string) (*sql.DB, error) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("schema: remove %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("schema: open %s: %w", path, err)
	}
	if _, err := db.Exec(ddl); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: apply DDL to %s: %w", path, err)
	}
	return db, nil
}
