// Package config reads settings from the environment and an optional .env file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is everything the app takes from the environment.
type Config struct {
	Addr          string   // WALLETFLOW_ADDR
	DBPath        string   // WALLETFLOW_DB
	EtherscanKey  string   // ETHERSCAN_API_KEY
	TronGridKey   string   // TRONGRID_API_KEY
	IgnoreChains  []string // IGNORE_CHAINS, comma separated chain keys
	MaxPerAddress int      // MAX_TRANSFERS_PER_ADDRESS, -1 = not set
	EnvFile       string   // the .env that was loaded, if any
}

// Load reads the first existing .env from paths into the process environment
// (real environment variables win) and returns the resulting Config.
func Load(paths ...string) (Config, error) {
	var c Config
	for _, p := range paths {
		err := loadDotEnv(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return c, err
		}
		c.EnvFile = p
		break
	}
	c.Addr = getenv("WALLETFLOW_ADDR", "127.0.0.1:8080")
	c.DBPath = os.Getenv("WALLETFLOW_DB")
	c.EtherscanKey = os.Getenv("ETHERSCAN_API_KEY")
	c.TronGridKey = os.Getenv("TRONGRID_API_KEY")
	c.MaxPerAddress = -1
	if v := strings.TrimSpace(os.Getenv("MAX_TRANSFERS_PER_ADDRESS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return c, fmt.Errorf("MAX_TRANSFERS_PER_ADDRESS: want a number >= 0, got %q", v)
		}
		c.MaxPerAddress = n
	}
	for k := range strings.SplitSeq(os.Getenv("IGNORE_CHAINS"), ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			c.IgnoreChains = append(c.IgnoreChains, k)
		}
	}
	return c, nil
}

// DataDir is where the database and an optional .env live by default.
func DataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "WalletFlow"), nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv parses KEY=VALUE lines. Supports comments, "export", and quoted values.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := parseLine(sc.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if n := len(value); n >= 2 && (value[0] == '"' && value[n-1] == '"' || value[0] == '\'' && value[n-1] == '\'') {
		value = value[1 : n-1]
	} else if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, key != ""
}
