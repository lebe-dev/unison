package store

import (
	"testing"
	"testing/fstest"
)

func TestMigrationsFromFS(t *testing.T) {
	fsys := fstest.MapFS{
		"db/0001_peers.sql":   {Data: []byte("CREATE TABLE peers (id BLOB);")},
		"db/0003_history.sql": {Data: []byte("CREATE TABLE history (id BLOB);")},
		"db/notes.txt":        {Data: []byte("ignored")},
	}

	migrations, err := MigrationsFromFS(fsys, "db", 100)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("migrations: got %d, want 2", len(migrations))
	}

	first := migrations[0]
	if first.Version != 101 || first.Name != "0001_peers" {
		t.Fatalf("first migration: got %d %q", first.Version, first.Name)
	}
	if want := "CREATE TABLE peers (id BLOB);"; first.SQL != want {
		t.Fatalf("first migration SQL: got %q, want %q", first.SQL, want)
	}
	if second := migrations[1]; second.Version != 103 || second.Name != "0003_history" {
		t.Fatalf("second migration: got %d %q", second.Version, second.Name)
	}
}

func TestMigrationsFromFSRejectsBadFiles(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
		dir  string
	}{
		{
			name: "no version",
			fsys: fstest.MapFS{"db/init.sql": {Data: []byte("SELECT 1;")}},
			dir:  "db",
		},
		{
			name: "version is not a number",
			fsys: fstest.MapFS{"db/x_init.sql": {Data: []byte("SELECT 1;")}},
			dir:  "db",
		},
		{
			name: "zero version",
			fsys: fstest.MapFS{"db/0000_init.sql": {Data: []byte("SELECT 1;")}},
			dir:  "db",
		},
		{
			name: "no files",
			fsys: fstest.MapFS{"db/readme.txt": {Data: []byte("none")}},
			dir:  "db",
		},
		{
			name: "missing directory",
			fsys: fstest.MapFS{},
			dir:  "db",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := MigrationsFromFS(test.fsys, test.dir, 0); err == nil {
				t.Fatal("read migrations: want an error")
			}
		})
	}
}

func TestBaselineMigrations(t *testing.T) {
	migrations, err := loadBaseline()
	if err != nil {
		t.Fatalf("read baseline migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("baseline migrations are empty")
	}
	if _, err := mergeMigrations(migrations); err == nil {
		t.Fatal("merge baseline with itself: want a duplicate version error")
	}
}
