package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Environment       string
	Port              string
	DatabaseURL       string
	JWTSecret         string
	JWTIssuer         string
	JWTAudience       string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	AllowedOrigins    string
	CookieSecure      bool
	GoogleMapsAPIKey  string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
}

func Load() (Config, error) {
	_ = godotenv.Overload()

	accessTTL, err := duration("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTTL, err := duration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	secure, err := strconv.ParseBool(value("COOKIE_SECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("COOKIE_SECURE must be true or false: %w", err)
	}

	dbMaxOpen, err := intEnv("DB_MAX_OPEN_CONNS", 10)
	if err != nil {
		return Config{}, fmt.Errorf("DB_MAX_OPEN_CONNS must be a positive integer: %w", err)
	}
	dbMaxIdle, err := intEnv("DB_MAX_IDLE_CONNS", 5)
	if err != nil {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS must be a positive integer: %w", err)
	}
	dbLifetime, err := minuteEnv("DB_CONN_MAX_LIFETIME_MINUTES", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	dbIdleTime, err := minuteEnv("DB_CONN_MAX_IDLE_TIME_MINUTES", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment:       value("APP_ENV", "development"),
		Port:              value("PORT", "8080"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		JWTIssuer:         value("JWT_ISSUER", "yummy-crm"),
		JWTAudience:       value("JWT_AUDIENCE", "yummy-crm-api"),
		AccessTokenTTL:    accessTTL,
		RefreshTokenTTL:   refreshTTL,
		AllowedOrigins:    value("ALLOWED_ORIGINS", "http://localhost:5173"),
		CookieSecure:      secure,
		GoogleMapsAPIKey:  strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")),
		DBMaxOpenConns:    dbMaxOpen,
		DBMaxIdleConns:    dbMaxIdle,
		DBConnMaxLifetime: dbLifetime,
		DBConnMaxIdleTime: dbIdleTime,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	return cfg, nil
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}
	return parsed, nil
}

func value(name, fallback string) string {
	if current := strings.TrimSpace(os.Getenv(name)); current != "" {
		return current
	}
	return fallback
}

func intEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return n, nil
}

func minuteEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of minutes, got %q", name, raw)
	}
	return time.Duration(n) * time.Minute, nil
}
