package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type LiteArgsDb struct {
	lock         *sync.Mutex
	db           *sql.DB
	columns      string
	placeholders string
}

func NewLiteArgsDb(file string) (*LiteArgsDb, error) {
	db, err := sql.Open("libsql", fmt.Sprintf("file:%v", file))
	if err != nil {
		return nil, fmt.Errorf("failed to open liteargs state db: %w", err)
	}
	liteArgsDb := &LiteArgsDb{lock: &sync.Mutex{}, db: db}
	if err = liteArgsDb.init(); err != nil {
		return nil, err
	}
	return liteArgsDb, nil
}

func (l *LiteArgsDb) init() error {
	result, err := l.db.Query(`
	SELECT name FROM pragma_table_info('liteargs') WHERE name NOT IN (
		'succeed', 'attempts', 'last_stdout', 'last_stderr', 'last_attempt_dt'
	)`)
	if err != nil {
		return fmt.Errorf("failed to load liteargs table info: %w", err)
	}
	defer result.Close()

	headers := make([]string, 0)
	for result.Next() {
		var column string
		err = result.Scan(&column)
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("failed to load liteargs table info: %w", err)
		}
		headers = append(headers, column)
	}
	l.columns = strings.Join(headers, ", ")
	l.placeholders = strings.Join(repeat("?", len(headers)), ", ")
	return nil
}

func (l *LiteArgsDb) Init(header []string) error {
	createStatement := fmt.Sprintf(`
					CREATE TABLE IF NOT EXISTS liteargs (
    						%v, 
    						succeed INT DEFAULT 0, 
    						attempts INT DEFAULT 0, 
    						last_stdout TEXT DEFAULT "",
    						last_stderr TEXT DEFAULT "",
    						last_attempt_dt TEXT DEFAULT ""
					)`, strings.Join(header, ", "))
	_, err := l.db.Exec(createStatement)
	if err != nil {
		return fmt.Errorf("failed to create liteargs table: %w", err)
	}
	l.columns = strings.Join(header, ", ")
	l.placeholders = strings.Join(repeat("?", len(header)), ", ")
	return nil
}

func (l *LiteArgsDb) Insert(record []string) error {
	insertStatement := fmt.Sprintf("INSERT INTO liteargs(%v) VALUES (%v)", l.columns, l.placeholders)
	_, err := l.db.Exec(insertStatement, anyArray(record)...)
	if err != nil {
		return fmt.Errorf("failed to insert record: %w", err)
	}
	return nil
}

func (l *LiteArgsDb) Reset() error {
	_, err := l.db.Exec(`UPDATE liteargs SET succeed = 0, attempts = 0, last_stdout = "", last_stderr = "", last_attempt_dt = ""`)
	if err != nil {
		return fmt.Errorf("failed to reset liteargs table state: %w", err)
	}
	return nil
}

func (l *LiteArgsDb) attempts(tx *sql.Tx, primaryKey any) (int, error) {
	rows, err := tx.Query(`SELECT attempts FROM liteargs WHERE rowid = ?`, primaryKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get liteargs attempts: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, fmt.Errorf("failed to find liteargs row: rowid=%v", primaryKey)
	}
	var attempts int
	err = rows.Scan(&attempts)
	if err != nil {
		return 0, fmt.Errorf("failed to parse attempts row: %w", err)
	}
	return attempts, nil
}

func (l *LiteArgsDb) Update(primaryKey any, succeed bool, stdout, stderr string, t time.Time) error {
	l.lock.Lock()
	defer l.lock.Unlock()

	tx, err := l.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to update liteargs row: %w", err)
	}
	attempts, err := l.attempts(tx, primaryKey)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to update liteargs row: %w", err)
	}
	_, err = tx.Exec(
		`UPDATE liteargs SET succeed = ?, attempts = ?, last_stdout = ?, last_stderr = ?, last_attempt_dt = ? WHERE rowid = ?`,
		succeed,
		attempts+1,
		stdout,
		stderr,
		t.Format(time.DateTime),
		primaryKey,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to update liteargs row: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit liteargs update: %w", err)
	}
	return nil
}

type LiteArgsDbFilter struct {
	Take   int
	Filter string
	Order  string
}

func (l *LiteArgsDb) Filter(filter LiteArgsDbFilter) ([]map[string]any, []any, error) {
	limit := filter.Take
	if limit == 0 {
		limit = -1
	}
	order := filter.Order
	if order == "" {
		order = "last_attempt_dt ASC"
	}
	where := filter.Filter
	if where == "" {
		where = "1 = 1"
	}
	where = fmt.Sprintf("(%v) AND succeed = 0", where)

	rows, err := l.db.Query(fmt.Sprintf(`SELECT rowid, %v FROM liteargs WHERE %v ORDER BY %v LIMIT %v`, l.columns, where, order, limit))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get liteargs rows: filter='%v', order='%v', limit='%v', err=%w", where, order, limit, err)
	}
	defer rows.Close()
	results := make([]map[string]any, 0)
	primaryKeys := make([]any, 0)
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get results columns: %w", err)
	}
	for rows.Next() {
		values := make([]any, len(columns))
		refs := make([]any, len(columns))
		for i := range refs {
			refs[i] = &values[i]
		}
		err = rows.Scan(refs...)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse litearg row: err=%w", err)
		}
		result := make(map[string]any, len(columns))
		for i, column := range columns {
			result[column] = values[i]
		}
		results = append(results, result)
		primaryKeys = append(primaryKeys, result["rowid"])
	}
	return results, primaryKeys, nil
}
