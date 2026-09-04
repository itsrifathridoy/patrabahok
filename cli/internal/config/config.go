// Package config reads the runtime configuration written by the installer:
// /etc/patrabahok/mysql-admin.cnf (a minimal INI-style file, [client] section
// with user/password/host/database keys — the same file the Bash CLI used).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const DefaultMySQLCnfPath = "/etc/patrabahok/mysql-admin.cnf"

type DBConfig struct {
	User     string
	Password string
	Host     string
	Database string
}

func LoadDBConfig(path string) (*DBConfig, error) {
	if path == "" {
		path = DefaultMySQLCnfPath
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w (has the installer finished the database phase?)", path, err)
	}
	defer f.Close()

	cfg := &DBConfig{Host: "127.0.0.1"}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "user":
			cfg.User = val
		case "password":
			cfg.Password = val
		case "host":
			cfg.Host = val
		case "database":
			cfg.Database = val
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if cfg.User == "" || cfg.Database == "" {
		return nil, fmt.Errorf("%s is missing required fields (user/database)", path)
	}
	return cfg, nil
}

// DSN builds a go-sql-driver/mysql data source name.
func (c *DBConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4", c.User, c.Password, c.Host, c.Database)
}
