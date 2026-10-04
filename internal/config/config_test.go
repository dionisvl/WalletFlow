package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		line, key, value string
		ok               bool
	}{
		{"A=1", "A", "1", true},
		{"export B = two ", "B", "two", true},
		{`C="x # y"`, "C", "x # y", true},
		{"D=val # note", "D", "val", true},
		{"# comment", "", "", false},
		{"garbage", "", "", false},
		{"E=", "E", "", true},
	}
	for _, tt := range tests {
		k, v, ok := parseLine(tt.line)
		if k != tt.key || v != tt.value || ok != tt.ok {
			t.Errorf("parseLine(%q) = %q, %q, %v", tt.line, k, v, ok)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	os.WriteFile(p, []byte("ETHERSCAN_API_KEY=from-file\nIGNORE_CHAINS=Base, bsc\nTRONGRID_API_KEY=file\n"), 0o600)
	t.Setenv("TRONGRID_API_KEY", "from-env")
	t.Setenv("ETHERSCAN_API_KEY", "")
	os.Unsetenv("ETHERSCAN_API_KEY")
	os.Unsetenv("IGNORE_CHAINS")
	c, err := Load(filepath.Join(dir, "missing.env"), p)
	if err != nil {
		t.Fatal(err)
	}
	if c.EtherscanKey != "from-file" || c.TronGridKey != "from-env" || c.EnvFile != p {
		t.Errorf("config = %+v", c)
	}
	if !slices.Equal(c.IgnoreChains, []string{"base", "bsc"}) {
		t.Errorf("ignore = %v", c.IgnoreChains)
	}
}
