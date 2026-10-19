package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"modernc.org/sqlite"
)

const sqliteTimeFormat = "2006-01-02 15:04:05.000000000-07:00"

// This driver only normalizes Go values (slices, JSON and UTC timestamps).
// Queries are native SQLite SQL, including statements executed in transactions.
type businessDriver struct{ driver.Driver }
type businessConn struct{ driver.Conn }

func (d *businessDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &businessConn{c}, nil
}
func (c *businessConn) CheckNamedValue(v *driver.NamedValue) error {
	var err error
	v.Value, err = sqliteValue(v.Value)
	return err
}
func sqliteValue(v any) (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	if val, ok := v.(driver.Valuer); ok {
		return val.Value()
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		return sqliteValue(rv.Elem().Interface())
	}
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(sqliteTimeFormat), nil
	case json.RawMessage:
		return string(x), nil
	case []byte:
		return string(x), nil
	case string:
		if strings.ContainsRune(x, 0) {
			return nil, errors.New("text contains a NUL character")
		}
	}
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map {
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return "[]", nil
		}
		b, err := json.Marshal(v)
		return string(b), err
	}
	return driver.DefaultParameterConverter.ConvertValue(v)
}
func (c *businessConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *businessConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *businessConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil {
		return nil, err
	}
	return &businessRows{r}, nil
}
func (c *businessConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if err != nil {
		return nil, err
	}
	return &businessStmt{s}, nil
}

type businessStmt struct{ driver.Stmt }

func (s *businessStmt) Query(v []driver.Value) (driver.Rows, error) {
	r, e := s.Stmt.Query(v)
	if e != nil {
		return nil, e
	}
	return &businessRows{r}, nil
}
func (s *businessStmt) QueryContext(ctx context.Context, v []driver.NamedValue) (driver.Rows, error) {
	r, e := s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, v)
	if e != nil {
		return nil, e
	}
	return &businessRows{r}, nil
}
func (s *businessStmt) ExecContext(ctx context.Context, v []driver.NamedValue) (driver.Result, error) {
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, v)
}

type businessRows struct{ driver.Rows }

func (r *businessRows) Next(dest []driver.Value) error {
	if err := r.Rows.Next(dest); err != nil {
		return err
	}
	names := r.Columns()
	for i, v := range dest {
		if s, ok := v.(string); ok {
			if r.ColumnTypeDatabaseTypeName(i) == "JSON" || (len(s) > 0 && (s[0] == '{' || s[0] == '[') && json.Valid([]byte(s))) {
				dest[i] = []byte(s)
				continue
			}
			name := strings.ToLower(names[i])
			if name == "last_used" || strings.Contains(name, "max(") || strings.Contains(name, "min(") {
				if t, e := parseSQLiteTime(s); e == nil {
					dest[i] = t
				}
			}
		}
	}
	return nil
}
func (r *businessRows) ColumnTypeDatabaseTypeName(i int) string {
	return r.Rows.(driver.RowsColumnTypeDatabaseTypeName).ColumnTypeDatabaseTypeName(i)
}
func (r *businessRows) ColumnTypeNullable(i int) (bool, bool) {
	return r.Rows.(driver.RowsColumnTypeNullable).ColumnTypeNullable(i)
}
func (r *businessRows) ColumnTypeLength(i int) (int64, bool) {
	return r.Rows.(driver.RowsColumnTypeLength).ColumnTypeLength(i)
}
func (r *businessRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	return r.Rows.(driver.RowsColumnTypePrecisionScale).ColumnTypePrecisionScale(i)
}
func (r *businessRows) ColumnTypeScanType(i int) reflect.Type {
	return r.Rows.(driver.RowsColumnTypeScanType).ColumnTypeScanType(i)
}

func init() {
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	sql.Register("artex-sqlite", &businessDriver{base.Driver()})
	base.Close()
	scalar("now", 0, false, func(a []driver.Value) (driver.Value, error) { return time.Now().UTC().Format(sqliteTimeFormat), nil })
	scalar("time_add", 2, true, func(a []driver.Value) (driver.Value, error) {
		if a[0] == nil || a[1] == nil {
			return nil, nil
		}
		t, err := parseSQLiteTime(fmt.Sprint(a[0]))
		if err != nil {
			return nil, err
		}
		var seconds float64
		switch v := a[1].(type) {
		case float64:
			seconds = v
		case int64:
			seconds = float64(v)
		default:
			return nil, fmt.Errorf("invalid time offset %v", v)
		}
		return t.Add(time.Duration(seconds * float64(time.Second))).UTC().Format(sqliteTimeFormat), nil
	})
	scalar("try_ip", 1, true, func(a []driver.Value) (driver.Value, error) {
		if a[0] == nil {
			return nil, nil
		}
		ip, e := netip.ParseAddr(fmt.Sprint(a[0]))
		if e != nil {
			return nil, nil
		}
		return ip.String(), nil
	})
	scalar("ip_contains", 2, true, func(a []driver.Value) (driver.Value, error) {
		if a[0] == nil || a[1] == nil {
			return nil, nil
		}
		p, e := netip.ParsePrefix(fmt.Sprint(a[0]))
		if e != nil {
			return nil, e
		}
		ip, e := netip.ParseAddr(fmt.Sprint(a[1]))
		if e != nil {
			return nil, e
		}
		return p.Contains(ip), nil
	})
	scalar("prefix_len", 1, true, func(a []driver.Value) (driver.Value, error) {
		if a[0] == nil {
			return nil, nil
		}
		p, e := netip.ParsePrefix(fmt.Sprint(a[0]))
		return int64(p.Bits()), e
	})
	scalar("regexp_replace", 4, true, func(a []driver.Value) (driver.Value, error) {
		if a[0] == nil {
			return nil, nil
		}
		r, e := regexp.Compile(fmt.Sprint(a[1]))
		if e != nil {
			return nil, e
		}
		return r.ReplaceAllString(fmt.Sprint(a[0]), fmt.Sprint(a[2])), nil
	})
	scalar("json_array_union", 2, true, func(a []driver.Value) (driver.Value, error) { return mergeJSONArray(a, "") })
	scalar("json_merge_ports", 2, true, func(a []driver.Value) (driver.Value, error) { return mergeJSONArray(a, "port") })
	scalar("json_merge_params", 2, true, func(a []driver.Value) (driver.Value, error) { return mergeJSONArray(a, "params") })
	scalar("json_array_remove", 2, true, func(a []driver.Value) (driver.Value, error) {
		items, e := jsonArray(a[0])
		if e != nil {
			return nil, e
		}
		out := make([]any, 0, len(items))
		for _, v := range items {
			if fmt.Sprint(v) != fmt.Sprint(a[1]) {
				out = append(out, v)
			}
		}
		return jsonString(out)
	})
	scalar("json_array_append", 2, true, func(a []driver.Value) (driver.Value, error) {
		items, e := jsonArray(a[0])
		if e != nil {
			return nil, e
		}
		return jsonString(append(items, a[1]))
	})
	scalar("json_merge", 2, true, func(a []driver.Value) (driver.Value, error) {
		if a[0] == nil || a[1] == nil {
			return nil, nil
		}
		var left, right map[string]any
		decoder := json.NewDecoder(strings.NewReader(fmt.Sprint(a[0])))
		decoder.UseNumber()
		if e := decoder.Decode(&left); e != nil {
			return nil, e
		}
		decoder = json.NewDecoder(strings.NewReader(fmt.Sprint(a[1])))
		decoder.UseNumber()
		if e := decoder.Decode(&right); e != nil {
			return nil, e
		}
		if left == nil {
			left = map[string]any{}
		}
		for k, v := range right {
			left[k] = v
		}
		return jsonString(left)
	})
	// Preserve Unicode case-insensitive search for LIKE, including ESCAPE.
	scalar("like", -1, true, sqliteLike)
}

func scalar(name string, n int32, deterministic bool, fn func([]driver.Value) (driver.Value, error)) {
	err := sqlite.RegisterFunction(name, &sqlite.FunctionImpl{NArgs: n, Deterministic: deterministic, Scalar: func(_ *sqlite.FunctionContext, a []driver.Value) (driver.Value, error) {
		v, err := fn(a)
		if b, ok := v.(bool); ok {
			if b {
				return int64(1), err
			}
			return int64(0), err
		}
		return v, err
	}})
	if err != nil {
		panic(err)
	}
}
func jsonString(v any) (driver.Value, error) { b, e := json.Marshal(v); return string(b), e }
func jsonArray(v driver.Value) ([]any, error) {
	if v == nil {
		return []any{}, nil
	}
	var out []any
	decoder := json.NewDecoder(strings.NewReader(fmt.Sprint(v)))
	decoder.UseNumber()
	err := decoder.Decode(&out)
	if out == nil {
		out = []any{}
	}
	return out, err
}
func mergeJSONArray(args []driver.Value, key string) (driver.Value, error) {
	left, e := jsonArray(args[0])
	if e != nil {
		return nil, e
	}
	right, e := jsonArray(args[1])
	if e != nil {
		return nil, e
	}
	byKey := map[string]any{}
	for _, v := range append(left, right...) {
		b, _ := json.Marshal(v)
		k := string(b)
		if key != "" {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("JSON array element must be an object")
			}
			if key == "port" {
				k = fmt.Sprint(m["port"])
			} else {
				k = fmt.Sprint(m["location"]) + "\x00" + fmt.Sprint(m["name"])
			}
			if prev, ok := byKey[k]; ok {
				if key == "port" {
					old := prev.(map[string]any)
					if fmt.Sprint(old["service"]) != "" && old["service"] != nil {
						continue
					}
				} else {
					pb, _ := json.Marshal(prev)
					if string(pb) > string(b) {
						continue
					}
				}
			}
		}
		byKey[k] = v
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return jsonString(out)
}
func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{sqliteTimeFormat, time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", s)
}
func sqliteLike(a []driver.Value) (driver.Value, error) {
	if a[0] == nil || a[1] == nil {
		return nil, nil
	}
	pattern := []rune(fmt.Sprint(a[0]))
	escape := rune(0)
	if len(a) == 3 {
		if a[2] == nil {
			return nil, nil
		}
		r := []rune(fmt.Sprint(a[2]))
		if len(r) != 1 {
			return nil, errors.New("ESCAPE must be one character")
		}
		escape = r[0]
	}
	var b strings.Builder
	b.WriteString("(?is)^")
	for i := 0; i < len(pattern); i++ {
		r := pattern[i]
		if escape != 0 && r == escape {
			if i+1 == len(pattern) {
				return false, nil
			}
			i++
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		} else {
			switch r {
			case '%':
				b.WriteString(".*")
			case '_':
				b.WriteByte('.')
			default:
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
	}
	b.WriteByte('$')
	r, e := regexp.Compile(b.String())
	if e != nil {
		return nil, e
	}
	return r.MatchString(fmt.Sprint(a[1])), nil
}

func IsUniqueViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && (e.Code() == 2067 || e.Code() == 1555)
}
func IsForeignKeyViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && e.Code() == 787
}
