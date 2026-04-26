package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DB struct {
	Path string
}

func Open(path string) (*DB, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, errors.New("sqlite3 binary is required in PATH")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	file, err := os.OpenFile(absolutePath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close database file: %w", err)
	}

	return &DB{Path: absolutePath}, nil
}

func (db *DB) Close() error {
	return nil
}

func (db *DB) Exec(ctx context.Context, script string) error {
	_, err := db.run(ctx, false, script)
	return err
}

func (db *DB) Query(ctx context.Context, script string, target any) error {
	output, err := db.run(ctx, true, script)
	if err != nil {
		return err
	}

	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		trimmed = []byte("[]")
	}

	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("decode sqlite json output: %w", err)
	}

	return nil
}

func (db *DB) run(ctx context.Context, jsonOutput bool, script string) ([]byte, error) {
	args := []string{}
	if jsonOutput {
		args = append(args, "-json")
	}
	args = append(args, db.Path)

	cmd := exec.CommandContext(ctx, "sqlite3", args...)
	cmd.Stdin = strings.NewReader(".bail on\n.timeout 5000\nPRAGMA foreign_keys = ON;\n" + normalizeScript(script) + "\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("sqlite3 failed with exit code %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("sqlite3 execution failed: %w", err)
	}

	return stdout.Bytes(), nil
}

func normalizeScript(script string) string {
	lines := strings.Split(strings.TrimSpace(script), "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}
