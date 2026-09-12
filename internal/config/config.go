// Package config reads runtime configuration from the environment.
package config

import (
	"errors"
	"os"
	"time"
)

type Config struct {
	DBPath          string
	Listen          string
	BaseURL         string
	Pepper          string
	Dev             bool
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	Location        *time.Location
}

func FromEnv() (Config, error) {
	c := Config{
		DBPath:          getenv("SB_DB_PATH", "data/so-budget.db"),
		Listen:          getenv("SB_LISTEN", ":8080"),
		BaseURL:         getenv("SB_BASE_URL", "http://localhost:8080"),
		Pepper:          os.Getenv("SB_PEPPER"),
		Dev:             os.Getenv("SB_DEV") == "1",
		IdleTimeout:     duration("SB_IDLE_TIMEOUT", 30*time.Minute),
		AbsoluteTimeout: duration("SB_ABS_TIMEOUT", 12*time.Hour),
	}
	loc, err := time.LoadLocation(getenv("SB_TZ", "America/Toronto"))
	if err != nil {
		return c, err
	}
	c.Location = loc
	if c.Pepper == "" {
		if !c.Dev {
			return c, errors.New("SB_PEPPER is required unless SB_DEV=1")
		}
		c.Pepper = "dev-pepper"
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func duration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
