package clip

import "testing"

func TestMigrationsUseTheClipBlock(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations")
	}
	if first := migrations[0].Version; first != migrationsBase+1 {
		t.Fatalf("first migration version is %d, want %d", first, migrationsBase+1)
	}
}
