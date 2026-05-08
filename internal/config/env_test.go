package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Satyaamm/plowered/internal/config"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDotEnvParsesAndExports(t *testing.T) {
	const k = "PLOWERED_TEST_LOAD"
	t.Setenv(k, "")
	os.Unsetenv(k)

	p := writeTemp(t, ".env", "PLOWERED_TEST_LOAD=hello\n")
	if err := config.LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(k); got != "hello" {
		t.Errorf("getenv = %q", got)
	}
}

func TestLoadDotEnvDoesNotOverrideExisting(t *testing.T) {
	const k = "PLOWERED_TEST_OVERRIDE"
	t.Setenv(k, "shell-set")

	p := writeTemp(t, ".env", k+"=file-set\n")
	if err := config.LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(k); got != "shell-set" {
		t.Errorf("expected shell value to win, got %q", got)
	}
}

func TestLoadDotEnvHandlesQuotesAndComments(t *testing.T) {
	t.Setenv("PLOWERED_T_A", "")
	t.Setenv("PLOWERED_T_B", "")
	os.Unsetenv("PLOWERED_T_A")
	os.Unsetenv("PLOWERED_T_B")

	p := writeTemp(t, ".env", `# top comment
PLOWERED_T_A="quoted value" # inline
PLOWERED_T_B=plain
`)
	if err := config.LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("PLOWERED_T_A"); got != "quoted value" {
		t.Errorf("A = %q", got)
	}
	if got := os.Getenv("PLOWERED_T_B"); got != "plain" {
		t.Errorf("B = %q", got)
	}
}

func TestLoadDotEnvMissingFileIsNotError(t *testing.T) {
	if err := config.LoadDotEnv("/no/such/.env"); err != nil {
		t.Errorf("missing file should be silently skipped, got %v", err)
	}
}

func TestLoadDotEnvMalformedReturnsError(t *testing.T) {
	p := writeTemp(t, ".env", "no_equals_sign\n")
	if err := config.LoadDotEnv(p); err == nil {
		t.Error("expected error for malformed line")
	}
}
