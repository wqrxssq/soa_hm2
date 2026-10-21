package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr       string
	DatabaseURL    string
	JWTSecret      []byte
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	OrderRateLimit time.Duration
}

func Load() (Config, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	accessTTL, err := durationEnv("ACCESS_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTTL, err := durationEnv("REFRESH_TTL", 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	rateLimit, err := durationEnv("ORDER_RATE_LIMIT", time.Minute)
	if err != nil {
		return Config{}, err
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return Config{
		HTTPAddr:       addr,
		DatabaseURL:    databaseURL,
		JWTSecret:      []byte(secret),
		AccessTTL:      accessTTL,
		RefreshTTL:     refreshTTL,
		OrderRateLimit: rateLimit,
	}, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}
