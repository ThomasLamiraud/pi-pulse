package metrics

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createDatabase(t *testing.T, path string, statements ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseDatabaseSpecs(t *testing.T) {
	specs, err := ParseDatabaseSpecs(" HomeMonitor=/home/pi/db/data.db , Pi-hole queries=/etc/pihole/pihole-FTL.db,")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].Name != "HomeMonitor" || specs[1].Name != "Pi-hole queries" || specs[1].Path != "/etc/pihole/pihole-FTL.db" {
		t.Errorf("specs = %+v", specs)
	}
	for _, invalid := range []string{"no-equals", "=/path", "name=relative/path"} {
		if _, err := ParseDatabaseSpecs(invalid); err == nil {
			t.Errorf("%q should be rejected", invalid)
		}
	}
	if specs, err := ParseDatabaseSpecs(""); err != nil || len(specs) != 0 {
		t.Errorf("empty value = %v, %v", specs, err)
	}
}

func TestCountsTablesInTheBackground(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temperature.db")
	createDatabase(t, path,
		`CREATE TABLE sensor_readings (id INTEGER PRIMARY KEY, value REAL)`,
		`INSERT INTO sensor_readings (value) VALUES (21.5), (21.7), (22.0)`,
		`CREATE TABLE "odd ""name""" (id INTEGER)`,
	)

	reader := NewDatabaseReader([]DatabaseSpec{{Name: "HomeMonitor", Path: path}})
	first := reader.Read()[0]
	if first.SizeBytes == nil || *first.SizeBytes == 0 || first.ModifiedAt == nil {
		t.Fatalf("first read = %+v, want size and modification time", first)
	}
	if first.Tables != nil {
		t.Error("tables should not be ready before the background count finishes")
	}

	reader.wait.Wait()
	second := reader.Read()[0]
	if second.Error != "" || len(second.Tables) != 2 || second.TablesCheckedAt == nil {
		t.Fatalf("second read = %+v", second)
	}
	if second.Tables[0].Name != `odd "name"` || second.Tables[1].Name != "sensor_readings" || second.Tables[1].Rows != 3 {
		t.Errorf("tables = %+v", second.Tables)
	}
}

func TestCountsAreRefreshedOnlyWhenDue(t *testing.T) {
	calls := 0
	clock := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "data.db")
	os.WriteFile(path, []byte("x"), 0o644)

	reader := NewDatabaseReader([]DatabaseSpec{{Name: "Data", Path: path}})
	reader.now = func() time.Time { return clock }
	reader.count = func(context.Context, string) ([]Table, error) {
		calls++
		return []Table{{Name: "t", Rows: int64(calls)}}, nil
	}

	reader.Read()
	reader.wait.Wait()
	reader.Read()
	reader.wait.Wait()
	if calls != 1 {
		t.Errorf("counted %d times within the refresh interval, want 1", calls)
	}

	clock = clock.Add(tableRefreshInterval)
	reader.Read()
	reader.wait.Wait()
	if calls != 2 {
		t.Errorf("counted %d times after the interval, want 2", calls)
	}
}

func TestReportsMissingAndUnreadableDatabases(t *testing.T) {
	directory := t.TempDir()
	unreadable := filepath.Join(directory, "secret.db")
	createDatabase(t, unreadable, `CREATE TABLE t (id INTEGER)`)
	os.Chmod(unreadable, 0o000)
	t.Cleanup(func() { os.Chmod(unreadable, 0o644) })

	reader := NewDatabaseReader([]DatabaseSpec{
		{Name: "Missing", Path: filepath.Join(directory, "missing.db")},
		{Name: "Secret", Path: unreadable},
	})
	reader.Read()
	reader.wait.Wait()
	databases := reader.Read()

	if databases[0].Error != "file not found" || databases[0].SizeBytes != nil {
		t.Errorf("missing = %+v", databases[0])
	}
	if os.Geteuid() != 0 {
		secret := databases[1]
		if secret.SizeBytes == nil || !strings.HasPrefix(secret.Error, "Can't read tables") {
			t.Errorf("unreadable = %+v, want its size with a table error", secret)
		}
	}
}

func TestSizeIncludesTheWriteAheadLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	os.WriteFile(path, make([]byte, 100), 0o644)
	os.WriteFile(path+"-wal", make([]byte, 50), 0o644)
	size, _, err := statDatabase(path)
	if err != nil || size != 150 {
		t.Errorf("size = %d, %v; want 150", size, err)
	}
}
