package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
)

const (
	FamilyEVM  = "evm"
	FamilyTron = "tron"
)

var errBadAddress = errors.New("unknown address format")

// normalizeAddress detects the address family and returns the canonical form.
func normalizeAddress(s string) (family, addr string, err error) {
	s = strings.TrimSpace(s)
	switch {
	case isEVMAddress(s):
		return FamilyEVM, strings.ToLower(s), nil
	case strings.HasPrefix(s, "T") && len(s) == 34:
		if _, err := tronDecode(s); err != nil {
			return "", "", err
		}
		return FamilyTron, s, nil
	}
	return "", "", errBadAddress
}

func isEVMAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

// shortAddr renders 0x12ab…9f3c.
func shortAddr(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:6] + "…" + s[len(s)-4:]
}

const b58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func b58Encode(b []byte) string {
	n := new(big.Int).SetBytes(b)
	var out []byte
	mod := new(big.Int)
	base := big.NewInt(58)
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, b58Alphabet[mod.Int64()])
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func b58Decode(s string) ([]byte, error) {
	n := new(big.Int)
	base := big.NewInt(58)
	for _, c := range s {
		i := strings.IndexRune(b58Alphabet, c)
		if i < 0 {
			return nil, errBadAddress
		}
		n.Mul(n, base).Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	for _, c := range s {
		if c != '1' {
			break
		}
		b = append([]byte{0}, b...)
	}
	return b, nil
}

func checksum4(b []byte) []byte {
	h1 := sha256.Sum256(b)
	h2 := sha256.Sum256(h1[:])
	return h2[:4]
}

// tronDecode returns the 21-byte payload (0x41 prefix) of a base58check address.
func tronDecode(s string) ([]byte, error) {
	b, err := b58Decode(s)
	if err != nil || len(b) != 25 || b[0] != 0x41 {
		return nil, errBadAddress
	}
	if !bytes.Equal(checksum4(b[:21]), b[21:]) {
		return nil, errors.New("bad tron address checksum")
	}
	return b[:21], nil
}

// tronFromHex converts "41..." hex (as returned by TronGrid) to base58.
func tronFromHex(h string) string {
	b, err := hex.DecodeString(strings.TrimPrefix(h, "0x"))
	if err != nil {
		return h
	}
	if len(b) == 20 {
		b = append([]byte{0x41}, b...)
	}
	if len(b) != 21 {
		return h
	}
	return b58Encode(append(b, checksum4(b)...))
}
