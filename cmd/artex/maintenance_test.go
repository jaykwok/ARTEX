package main

import (
	"github.com/Autumn-27/artex/db"
	"golang.org/x/crypto/bcrypt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLitePasswordRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "business.sqlite")
	t.Setenv("ARTEX_DB_PATH", path)
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err = resetAdminPassword("", strings.NewReader("test-password-123\n")); err != nil {
		t.Fatal(err)
	}
	store, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hash, ok, err := store.GetSetting("auth.password_hash")
	if err != nil || !ok {
		t.Fatalf("password missing: %v", err)
	}
	if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte("test-password-123")); err != nil {
		t.Fatal(err)
	}
	if err = resetAdminPassword("", strings.NewReader("short\n")); err == nil {
		t.Fatal("short password accepted")
	}
}
func TestPasswordRecoveryDoesNotCreateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	t.Setenv("ARTEX_DB_PATH", path)
	if err := resetAdminPassword("", strings.NewReader("test-password-123\n")); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing database was created")
	}
}
