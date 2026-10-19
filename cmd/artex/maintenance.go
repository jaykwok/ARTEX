package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Autumn-27/artex/config"
	"github.com/Autumn-27/artex/db"
	"golang.org/x/crypto/bcrypt"
)

// Password input is read from stdin, never a command-line argument.
func resetAdminPassword(dataDir string, input io.Reader) error {
	password, err := bufio.NewReader(io.LimitReader(input, 1024)).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
	if utf8.RuneCountInString(password) < 8 || len(password) > 72 {
		return fmt.Errorf("密码至少 8 个字符，且不超过 72 字节")
	}
	path, source, err := config.SQLitePath()
	if err != nil {
		return err
	}
	if source == "默认 SQLite 文件" {
		path = filepath.Join(dataDir, "artex.sqlite")
	}
	if _, err = os.Stat(path); err != nil {
		return fmt.Errorf("existing database required: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	store, err := db.Open(path)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.SetSetting("auth.password_hash", string(hash))
}
