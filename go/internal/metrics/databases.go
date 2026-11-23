package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	tableRefreshInterval = 15 * time.Minute
	tableCountTimeout    = 20 * time.Second
	maxTablesPerDatabase = 20
)

// DatabaseSpec names a SQLite file to watch.
type DatabaseSpec struct {
	Name string
	Path string
}

// Database is what the dashboard shows for one SQLite file.
type Database struct {
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	SizeBytes  *int64     `json:"size_bytes"`
	ModifiedAt *time.Time `json:"modified_at"`
	// Tables lists row counts, refreshed every 15 minutes. It is nil until
	// the first count finishes.
	Tables          []Table    `json:"tables"`
	TablesCheckedAt *time.Time `json:"tables_checked_at"`
	Error           string     `json:"error,omitempty"`
}

// Table is one table and its row count.
type Table struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// ParseDatabaseSpecs parses WATCH_DATABASES, a comma-separated list of
// name=path pairs such as "HomeMonitor=/home/pi/app/db/data.db".
func ParseDatabaseSpecs(value string) ([]DatabaseSpec, error) {
	var specs []DatabaseSpec
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, path, found := strings.Cut(entry, "=")
		name, path = strings.TrimSpace(name), strings.TrimSpace(path)
		if !found || name == "" || !strings.HasPrefix(path, "/") {
			return nil, fmt.Errorf("invalid database %q: use name=/absolute/path", entry)
		}
		specs = append(specs, DatabaseSpec{Name: name, Path: path})
	}
	return specs, nil
}

// DatabaseReader reports the size and last write of SQLite files on every
// sample, and counts their rows in the background so a large table never
// delays sampling.
type DatabaseReader struct {
	specs []DatabaseSpec
	now   func() time.Time
	count func(ctx context.Context, path string) ([]Table, error)

	mu      sync.Mutex
	counts  map[string]tableCounts
	running map[string]bool
	wait    sync.WaitGroup
}

type tableCounts struct {
	tables    []Table
	checkedAt time.Time
	err       error
}

// NewDatabaseReader returns a reader for specs.
func NewDatabaseReader(specs []DatabaseSpec) *DatabaseReader {
	return &DatabaseReader{
		specs:   specs,
		now:     time.Now,
		count:   countTables,
		counts:  map[string]tableCounts{},
		running: map[string]bool{},
	}
}

// Read returns every watched database. It never blocks on row counts:
// those come from the last background refresh.
func (d *DatabaseReader) Read() []Database {
	databases := make([]Database, 0, len(d.specs))
	for _, spec := range d.specs {
		database := Database{Name: spec.Name, Path: spec.Path}
		size, modified, err := statDatabase(spec.Path)
		if err != nil {
			database.Error = describeError(err)
			databases = append(databases, database)
			continue
		}
		database.SizeBytes, database.ModifiedAt = &size, &modified

		d.mu.Lock()
		counts, counted := d.counts[spec.Path]
		due := !counted || d.now().Sub(counts.checkedAt) >= tableRefreshInterval
		if due && !d.running[spec.Path] {
			d.running[spec.Path] = true
			d.wait.Add(1)
			go d.refresh(spec.Path)
		}
		d.mu.Unlock()

		if counted {
			checkedAt := counts.checkedAt.UTC()
			database.TablesCheckedAt = &checkedAt
			if counts.err != nil {
				database.Error = "Can't read tables: " + describeError(counts.err)
			} else {
				database.Tables = counts.tables
			}
		}
		databases = append(databases, database)
	}
	return databases
}

func (d *DatabaseReader) refresh(path string) {
	defer d.wait.Done()
	ctx, cancel := context.WithTimeout(context.Background(), tableCountTimeout)
	defer cancel()
	tables, err := d.count(ctx, path)
	if err != nil {
		slog.Warn("counting database rows failed", "path", path, "error", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.counts[path] = tableCounts{tables: tables, checkedAt: d.now(), err: err}
	d.running[path] = false
}

// statDatabase returns the size of the database and its write-ahead log,
// and the time of the most recent write to either.
func statDatabase(path string) (int64, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	size, modified := info.Size(), info.ModTime()
	if wal, err := os.Stat(path + "-wal"); err == nil {
		size += wal.Size()
		if wal.ModTime().After(modified) {
			modified = wal.ModTime()
		}
	}
	return size, modified.UTC(), nil
}

// countTables opens the database read-only and counts the rows of each
// table. If the live file cannot be opened read-only (a WAL database in a
// directory this process cannot write), it reads it as an immutable file,
// which may miss the most recent writes.
func countTables(ctx context.Context, path string) ([]Table, error) {
	tables, err := countTablesWith(ctx, "file:"+path+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(2000)")
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		return tables, err
	}
	if immutable, immutableErr := countTablesWith(ctx, "file:"+path+"?mode=ro&immutable=1"); immutableErr == nil {
		return immutable, nil
	}
	return nil, err
}

func countTablesWith(ctx context.Context, dsn string) ([]Table, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name LIMIT ?`,
		maxTablesPerDatabase)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tables := make([]Table, 0, len(names))
	for _, name := range names {
		var count int64
		query := `SELECT COUNT(*) FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, err
		}
		tables = append(tables, Table{Name: name, Rows: count})
	}
	return tables, nil
}

func describeError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "file not found"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case strings.Contains(err.Error(), "unable to open"):
		return "can't open the file"
	case errors.Is(err, context.DeadlineExceeded):
		return "counting took too long"
	default:
		return err.Error()
	}
}
