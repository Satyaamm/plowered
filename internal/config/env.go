// Package config is the single boundary that turns process environment into
// typed configuration. Other packages depend on this — never on os.Getenv —
// so the source of truth is one file (.env) plus the orchestrator overrides.
//
// Loading order, first match wins:
//
//  1. process environment (set by the shell, container, or k8s)
//  2. .env.local (per-developer overrides; gitignored)
//  3. .env       (committed defaults; gitignored except for .env.example)
//
// We deliberately avoid pulling in github.com/joho/godotenv — env parsing is
// 30 lines and one fewer dependency is worth more than that convenience.
package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadDotEnv reads the supplied paths in order and exports every KEY=VALUE
// pair to the process environment, without overwriting variables already set
// by the shell. Missing files are skipped silently; malformed lines return an
// error so config bugs are loud, not silent.
func LoadDotEnv(paths ...string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("config: open %s: %w", p, err)
		}
		err = parseInto(f, func(k, v string) {
			if _, set := os.LookupEnv(k); !set {
				_ = os.Setenv(k, v)
			}
		})
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("config: parse %s: %w", p, err)
		}
	}
	return nil
}

// LoadDefault loads .env.local then .env from the current working directory.
// Call once near the top of main; subsequent reads via os.Getenv work as
// usual.
func LoadDefault() error {
	return LoadDotEnv(".env.local", ".env")
}

// parseInto reads KEY=VALUE lines and invokes set for each. It tolerates:
//   - blank lines
//   - "# comment" lines
//   - "KEY=value with spaces"
//   - "KEY=" (empty value)
//   - "KEY='quoted'" / "KEY=\"quoted\""
//
// It rejects lines that do not contain '=' as a syntax error.
func parseInto(r io.Reader, set func(k, v string)) error {
	sc := bufio.NewScanner(r)
	lineno := 0
	for sc.Scan() {
		lineno++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return fmt.Errorf("line %d: expected KEY=VALUE", lineno)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = stripInlineComment(val)
		val = unquote(val)
		set(key, val)
	}
	return sc.Err()
}

func stripInlineComment(v string) string {
	// Only strip when the # is preceded by whitespace and not inside quotes.
	if v == "" {
		return v
	}
	if v[0] == '\'' || v[0] == '"' {
		return v
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			return strings.TrimRight(v[:i], " \t")
		}
	}
	return v
}

func unquote(v string) string {
	if len(v) >= 2 {
		first, last := v[0], v[len(v)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
