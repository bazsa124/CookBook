// Package config resolves where data lives and how the server is reached.
//
// Precedence: flags > environment > <data>/config.json > defaults. This is the
// only place that knows about per-OS locations.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const DefaultPort = 8738 // Remoter owns 8737 on the same tailnet

type Config struct {
	DataDir string   `json:"-"`
	Port    int      `json:"port,omitempty"`
	Listen  []string `json:"listen,omitempty"` // explicit host:port list; empty = loopback + Tailscale
	Typst   string   `json:"typst,omitempty"`
	FFmpeg  string   `json:"ffmpeg,omitempty"` // optional: video poster frames

	GeminiKey   string `json:"gemini_api_key,omitempty"`
	GeminiModel string `json:"gemini_model,omitempty"`
	GroqKey     string `json:"groq_api_key,omitempty"`
	GroqModel   string `json:"groq_model,omitempty"`

	// Feeds is the automatic web import section, parsed by package feeds.
	Feeds json.RawMessage `json:"feeds,omitempty"`

	Token string `json:"-"`
}

// DefaultDataDir is %ProgramData%\CookBook on Windows (readable by the
// SYSTEM scheduled task and by you), /var/lib/cookbook for a root/systemd
// service on Linux, and ~/.local/share/cookbook otherwise.
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "CookBook")
		}
	}
	if os.Geteuid() == 0 {
		return filepath.Join(string(filepath.Separator), "var", "lib", "cookbook")
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "cookbook")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "cookbook")
	}
	return "cookbook-data"
}

// Load reads config.json from dataDir and applies environment overrides.
func Load(dataDir string) (*Config, error) {
	if dataDir == "" {
		dataDir = os.Getenv("COOKBOOK_DATA")
	}
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("data dir %s: %w", abs, err)
	}
	c := &Config{DataDir: abs, Port: DefaultPort}

	if b, err := os.ReadFile(filepath.Join(abs, "config.json")); err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("config.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if v := os.Getenv("COOKBOOK_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := os.Getenv("COOKBOOK_LISTEN"); v != "" {
		c.Listen = splitList(v)
	}
	env(&c.Typst, "TYPST_BIN")
	env(&c.FFmpeg, "FFMPEG_BIN")
	env(&c.GeminiKey, "GEMINI_API_KEY")
	env(&c.GeminiModel, "GEMINI_MODEL")
	env(&c.GroqKey, "GROQ_API_KEY")
	env(&c.GroqModel, "GROQ_MODEL")

	c.Token = os.Getenv("COOKBOOK_TOKEN")
	if c.Token == "" {
		if c.Token, err = loadOrCreateToken(filepath.Join(abs, "token")); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func env(dst *string, name string) {
	if v := os.Getenv(name); v != "" {
		*dst = v
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("writing token: %w", err)
	}
	return t, nil
}
