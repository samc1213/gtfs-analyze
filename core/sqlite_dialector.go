package core

import (
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// sqliteDialector fixes batch inserts of structs with `default:` tags.
//
// When some rows in a batch set a defaulted field and others leave it zero, gorm asks
// the dialector what to write for the zero rows. The stock SQLite dialector answers
// `DEFAULT`, which SQLite doesn't accept inside a multi-row VALUES list, so the insert
// fails with a syntax error. Instead, write the field's declared default directly;
// every such field in this project is `default:null`.
type sqliteDialector struct {
	*sqlite.Dialector
}

func newSqliteDialector(dsn string) sqliteDialector {
	return sqliteDialector{&sqlite.Dialector{DSN: dsn}}
}

func (dialector sqliteDialector) DefaultValueOf(field *schema.Field) clause.Expression {
	defaultValue := strings.TrimSpace(field.DefaultValue)
	if field.AutoIncrement || defaultValue == "" {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: defaultValue}
}
