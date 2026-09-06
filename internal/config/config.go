package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen             string
	DataDir            string
	DatabasePath       string
	MasterKeyFile      string
	PublicURL          string
	AllowedOrigins     []string
	CookieSecure       bool
	AdminUser          string
	AdminPassword      string
	ConnectTimeout     time.Duration
	HandshakeTimeout   time.Duration
	KeepaliveInterval  time.Duration
	SessionIdleTimeout time.Duration
	MaxMessageBytes    int64
	MaxSessionsPerUser int
	MaxSessionsTotal   int
	DefaultTerm        string
}

func Load() (Config, error) {
	dataDir := env("WEBSSH_DATA_DIR", "./data")
	c := Config{
		Listen: env("WEBSSH_LISTEN", "0.0.0.0:8080"), DataDir: dataDir,
		DatabasePath:   env("WEBSSH_DATABASE", filepath.Join(dataDir, "webssh.db")),
		MasterKeyFile:  env("WEBSSH_MASTER_KEY_FILE", filepath.Join(dataDir, "master.key")),
		PublicURL:      os.Getenv("WEBSSH_PUBLIC_URL"),
		AllowedOrigins: csv(os.Getenv("WEBSSH_ALLOWED_ORIGINS")),
		CookieSecure:   envBool("WEBSSH_COOKIE_SECURE", false),
		AdminUser:      env("WEBSSH_ADMIN_USER", "admin"), AdminPassword: os.Getenv("WEBSSH_ADMIN_PASSWORD"),
		ConnectTimeout:     envDuration("WEBSSH_CONNECT_TIMEOUT", 10*time.Second),
		HandshakeTimeout:   envDuration("WEBSSH_HANDSHAKE_TIMEOUT", 15*time.Second),
		KeepaliveInterval:  envDuration("WEBSSH_KEEPALIVE_INTERVAL", 30*time.Second),
		SessionIdleTimeout: envDuration("WEBSSH_SESSION_IDLE_TIMEOUT", 8*time.Hour),
		MaxMessageBytes:    envInt64("WEBSSH_MAX_WS_MESSAGE_BYTES", 1<<20),
		MaxSessionsPerUser: int(envInt64("WEBSSH_MAX_SESSIONS_PER_USER", 10)),
		MaxSessionsTotal:   int(envInt64("WEBSSH_MAX_SESSIONS_TOTAL", 50)),
		DefaultTerm:        env("WEBSSH_DEFAULT_TERM", "xterm-256color"),
	}
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return c, fmt.Errorf("create data dir: %w", err)
	}
	return c, nil
}

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
func csv(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func envBool(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	b, e := strconv.ParseBool(v)
	if e != nil {
		return d
	}
	return b
}
func envDuration(k string, d time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	x, e := time.ParseDuration(v)
	if e != nil {
		return d
	}
	return x
}
func envInt64(k string, d int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	x, e := strconv.ParseInt(v, 10, 64)
	if e != nil {
		return d
	}
	return x
}
