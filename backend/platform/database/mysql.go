package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	}
}

func ConnectMySQL(ctx context.Context, dsn string, pool PoolConfig) (*sql.DB, error) {
	if err := validateMySQLDSN(dsn); err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql connection: %w", err)
	}

	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
}

func validateMySQLDSN(dsn string) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse mysql dsn: %w", err)
	}
	if cfg.DBName == "" {
		return fmt.Errorf("mysql dsn must include database name")
	}
	if cfg.Net != "" && cfg.Addr == "" {
		return fmt.Errorf("mysql dsn must include network address")
	}

	values, err := parseDSNQuery(dsn)
	if err != nil {
		return err
	}
	if !allQueryValuesEqual(values, "charset", "utf8mb4") {
		return fmt.Errorf("mysql dsn must include charset=utf8mb4")
	}
	if !allQueryValuesEqual(values, "collation", "utf8mb4_0900_ai_ci") {
		return fmt.Errorf("mysql dsn must include collation=utf8mb4_0900_ai_ci")
	}
	if !cfg.ParseTime {
		return fmt.Errorf("mysql dsn must enable parseTime=true")
	}
	if cfg.Loc == nil || cfg.Loc.String() != "UTC" {
		return fmt.Errorf("mysql dsn must use loc=UTC")
	}
	if cfg.MultiStatements {
		return fmt.Errorf("mysql dsn must disable multiStatements")
	}
	return nil
}

func parseDSNQuery(rawDSN string) (url.Values, error) {
	queryStart := strings.LastIndex(rawDSN, "?")
	if queryStart < 0 {
		return nil, fmt.Errorf("mysql dsn must include query parameters")
	}
	values, err := url.ParseQuery(rawDSN[queryStart+1:])
	if err != nil {
		return nil, fmt.Errorf("parse mysql dsn query: %w", err)
	}
	return values, nil
}

func allQueryValuesEqual(values url.Values, key, expected string) bool {
	current, ok := values[key]
	if !ok || len(current) == 0 {
		return false
	}
	for _, value := range current {
		if value != expected {
			return false
		}
	}
	return true
}
