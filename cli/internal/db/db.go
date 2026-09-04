// Package db opens the shared MariaDB connection used by both the CLI and the API daemon.
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"

	"github.com/itsrifathridoy/patrabahok/cli/internal/config"
)

func Open(cfgPath string) (*sql.DB, error) {
	cfg, err := config.LoadDBConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	conn.SetMaxOpenConns(8)
	conn.SetMaxIdleConns(4)
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return conn, nil
}
