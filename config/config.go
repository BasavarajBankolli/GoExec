package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port             string
	WorkerCount      int
	JobQueueSize     int
	DefaultCPUQuota  float64
	DefaultMemoryMB  int64
	DefaultTimeout   time.Duration
	DefaultPidsLimit int64
	CacheTTL         time.Duration
	MetricsToken     string
}

func Load() *Config {
	return &Config{
		Port:             getEnv("PORT", "8080"),
		WorkerCount:      getEnvInt("WORKER_COUNT", 10),
		JobQueueSize:     getEnvInt("JOB_QUEUE_SIZE", 100),
		DefaultCPUQuota:  getEnvFloat("DEFAULT_CPU_QUOTA", 1.0),
		DefaultMemoryMB:  int64(getEnvInt("DEFAULT_MEMORY_MB", 128)),
		DefaultTimeout:   getEnvDuration("DEFAULT_TIMEOUT", 15*time.Second),
		DefaultPidsLimit: int64(getEnvInt("DEFAULT_PIDS_LIMIT", 64)),
		CacheTTL:         getEnvDuration("CACHE_TTL", 10*time.Minute),
		MetricsToken:     getEnv("METRICS_TOKEN", "secret-token"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
