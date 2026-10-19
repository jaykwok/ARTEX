package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

type sqlQueryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

var sqlIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type columnInfo struct{ name, typ string }

func sqliteColumns(q sqlQueryer, table string) ([]columnInfo, error) {
	if !sqlIdentifier.MatchString(table) {
		return nil, fmt.Errorf("invalid table %q", table)
	}
	rows, err := q.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []columnInfo
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			return nil, err
		}
		out = append(out, columnInfo{name, typ})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	return out, nil
}

// Row serialization is independent of the SQL engine and preserves native JSON,
// booleans and UTC timestamps used by existing v1-v3 task archive packages.
type archiveJSONRows struct {
	rows    *sql.Rows
	columns []*sql.ColumnType
}

func newArchiveJSONRows(rows *sql.Rows) (*archiveJSONRows, error) {
	columns, err := rows.ColumnTypes()
	return &archiveJSONRows{rows, columns}, err
}
func (r *archiveJSONRows) Next() bool { return r.rows.Next() }
func (r *archiveJSONRows) Err() error { return r.rows.Err() }
func (r *archiveJSONRows) Scan(dest ...any) error {
	values := make([]any, len(r.columns))
	targets := make([]any, len(values))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := r.rows.Scan(targets...); err != nil {
		return err
	}
	row := map[string]any{}
	for i, c := range r.columns {
		v := values[i]
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		if v != nil {
			switch strings.ToUpper(c.DatabaseTypeName()) {
			case "JSON":
				raw := []byte(fmt.Sprint(v))
				if !json.Valid(raw) {
					return fmt.Errorf("invalid JSON in column %s", c.Name())
				}
				v = json.RawMessage(raw)
			case "BOOLEAN":
				switch b := v.(type) {
				case int64:
					v = b != 0
				}
			case "DATETIME":
				if s, ok := v.(string); ok {
					t, err := parseSQLiteTime(s)
					if err != nil {
						return err
					}
					v = t.UTC()
				}
			}
		}
		row[c.Name()] = v
	}
	b, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if len(dest) != 1 {
		return fmt.Errorf("JSON row requires one destination")
	}
	switch p := dest[0].(type) {
	case *[]byte:
		*p = b
	case *string:
		*p = string(b)
	default:
		return fmt.Errorf("unsupported JSON row destination %T", p)
	}
	return nil
}

func queryJSONRows(q sqlQueryer, query string, args ...any) (json.RawMessage, int64, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	wrapped, err := newArchiveJSONRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return encodeArchiveRows(wrapped)
}
func streamJSONRows(q sqlQueryer, w io.Writer, query string, args ...any) (int64, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	wrapped, err := newArchiveJSONRows(rows)
	if err != nil {
		return 0, err
	}
	return writeArchiveRows(wrapped, w)
}

func prepareJSONRow(q sqlQueryer, table string, row map[string]any) ([]string, []any, error) {
	cols, err := sqliteColumns(q, table)
	if err != nil {
		return nil, nil, err
	}
	known := map[string]string{}
	for _, c := range cols {
		known[c.name] = c.typ
	}
	keys := make([]string, 0, len(row))
	for key := range row {
		if _, ok := known[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	vals := make([]any, len(keys))
	for i, key := range keys {
		v := row[key]
		if v != nil {
			switch strings.ToUpper(known[key]) {
			case "JSON":
				b, e := json.Marshal(v)
				if e != nil {
					return nil, nil, e
				}
				v = string(b)
			case "DATETIME":
				s, ok := v.(string)
				if !ok {
					if _, ok = v.(time.Time); !ok {
						return nil, nil, fmt.Errorf("invalid timestamp in %s.%s", table, key)
					}
				} else {
					t, e := parseSQLiteTime(s)
					if e != nil {
						return nil, nil, e
					}
					v = t
				}
			}
		}
		vals[i] = v
	}
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("no known columns in %s row", table)
	}
	return keys, vals, nil
}
func insertJSONRow(tx *sql.Tx, table string, row map[string]any, ignore bool) (sql.Result, error) {
	keys, vals, err := prepareJSONRow(tx, table, row)
	if err != nil {
		return nil, err
	}
	placeholders := make([]string, len(keys))
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = `"` + k + `"`
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	query := `INSERT INTO "` + table + `" (` + strings.Join(quoted, ",") + `) VALUES (` + strings.Join(placeholders, ",") + ")"
	if ignore {
		query += " ON CONFLICT DO NOTHING"
	}
	return tx.Exec(query, vals...)
}
func insertJSONRows(tx *sql.Tx, table string, raw json.RawMessage, ignore bool) error {
	rows, err := decodeArchiveRows(raw)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err = insertJSONRow(tx, table, row, ignore); err != nil {
			return fmt.Errorf("restore %s: %w", table, err)
		}
	}
	return nil
}
func updateJSONRow(tx *sql.Tx, table string, id int64, row map[string]any) error {
	delete(row, "id")
	keys, vals, err := prepareJSONRow(tx, table, row)
	if err != nil {
		return err
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf(`"%s"=$%d`, k, i+1)
	}
	vals = append(vals, id)
	_, err = tx.Exec(`UPDATE "`+table+`" SET `+strings.Join(parts, ",")+fmt.Sprintf(" WHERE id=$%d", len(vals)), vals...)
	return err
}
