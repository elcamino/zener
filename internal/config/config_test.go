// Zener - a tiny anonymous file dropbox.
// Copyright (C) 2026 Tobias von Dewitz <tobias@vondewitz.org>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package config_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/tob/zener/internal/config"
)

func TestLoadFromLookupRejectsMissingRequiredValues(t *testing.T) {
	_, err := config.LoadFromLookup(func(string) (string, bool) {
		return "", false
	})

	if err == nil {
		t.Fatal("expected missing configuration to fail")
	}
	for _, want := range []string{"BASE_URL", "SESSION_SECRET", "ADMIN_PASSWORD", "S3_ENDPOINT", "S3_BUCKET"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to mention %s, got %q", want, err.Error())
		}
	}
}

func TestLoadFromLookupAcceptsPasswordHashWithoutPlaintext(t *testing.T) {
	values := baseValues()
	delete(values, "ADMIN_PASSWORD")
	values["ADMIN_PASSWORD_HASH"] = "$2a$10$u2jdWdEhSWnztZ0ynTflA.X2tqztNA25sWwliWeqTCvS5Dj5slUaC"

	cfg, err := config.LoadFromLookup(lookupFrom(values))
	if err != nil {
		t.Fatalf("LoadFromLookup failed: %v", err)
	}
	if cfg.AdminPassword != "" {
		t.Fatalf("expected empty AdminPassword, got %q", cfg.AdminPassword)
	}
	if cfg.AdminPasswordHash == "" {
		t.Fatal("expected AdminPasswordHash to be set")
	}
}

func TestLoadFromLookupRequiresPasswordOrHash(t *testing.T) {
	values := baseValues()
	delete(values, "ADMIN_PASSWORD")

	_, err := config.LoadFromLookup(lookupFrom(values))
	if err == nil {
		t.Fatal("expected missing admin password to fail")
	}
	if !strings.Contains(err.Error(), "ADMIN_PASSWORD or ADMIN_PASSWORD_HASH") {
		t.Fatalf("expected error to mention password/hash requirement, got %q", err.Error())
	}
}

func baseValues() map[string]string {
	secret := base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	return map[string]string{
		"BASE_URL":       "https://zener.example.test",
		"SESSION_SECRET": secret,
		"ADMIN_PASSWORD": "super-secret",
		"S3_ENDPOINT":    "https://s3.example.test",
		"S3_REGION":      "eu-central-1",
		"S3_BUCKET":      "zener",
		"S3_ACCESS_KEY":  "access-key",
		"S3_SECRET_KEY":  "secret-key",
	}
}

func lookupFrom(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

func TestLoadFromLookupParsesTrustedProxyHops(t *testing.T) {
	values := baseValues()
	values["TRUSTED_PROXY_HOPS"] = "2"
	cfg, err := config.LoadFromLookup(lookupFrom(values))
	if err != nil {
		t.Fatalf("LoadFromLookup failed: %v", err)
	}
	if cfg.TrustedProxyHops != 2 {
		t.Fatalf("expected TrustedProxyHops 2, got %d", cfg.TrustedProxyHops)
	}
}

func TestLoadFromLookupParsesE2EIntakeConfig(t *testing.T) {
	values := baseValues()
	values["E2E_INTAKE_ENABLED"] = "true"
	values["E2E_INTAKE_REQUIRED"] = "true"
	values["E2E_INTAKE_ALGORITHM"] = "ML-KEM-1024-P384-HKDF-SHA512-AES-256-GCM"

	cfg, err := config.LoadFromLookup(lookupFrom(values))
	if err != nil {
		t.Fatalf("LoadFromLookup failed: %v", err)
	}

	if !cfg.E2EIntake.Enabled {
		t.Fatal("expected E2E intake to be enabled")
	}
	if !cfg.E2EIntake.Required {
		t.Fatal("expected E2E intake to be required")
	}
	if cfg.E2EIntake.Algorithm != "ML-KEM-1024-P384-HKDF-SHA512-AES-256-GCM" {
		t.Fatalf("unexpected E2E algorithm %q", cfg.E2EIntake.Algorithm)
	}
}

func TestLoadFromLookupRejectsE2ERequiredWhenDisabled(t *testing.T) {
	values := baseValues()
	values["E2E_INTAKE_ENABLED"] = "false"
	values["E2E_INTAKE_REQUIRED"] = "true"

	_, err := config.LoadFromLookup(lookupFrom(values))
	if err == nil {
		t.Fatal("expected E2E required without E2E enabled to fail")
	}
	if !strings.Contains(err.Error(), "E2E_INTAKE_REQUIRED") {
		t.Fatalf("expected E2E_INTAKE_REQUIRED error, got %q", err.Error())
	}
}

func TestLoadFromLookupRejectsUnsupportedE2EAlgorithm(t *testing.T) {
	values := baseValues()
	values["E2E_INTAKE_ENABLED"] = "true"
	values["E2E_INTAKE_ALGORITHM"] = "RSA-OAEP-AES-GCM"

	_, err := config.LoadFromLookup(lookupFrom(values))
	if err == nil {
		t.Fatal("expected unsupported E2E algorithm to fail")
	}
	if !strings.Contains(err.Error(), "E2E_INTAKE_ALGORITHM") {
		t.Fatalf("expected E2E_INTAKE_ALGORITHM error, got %q", err.Error())
	}
}

func TestLoadFromLookupRejectsNegativeTrustedProxyHops(t *testing.T) {
	values := baseValues()
	values["TRUSTED_PROXY_HOPS"] = "-1"
	if _, err := config.LoadFromLookup(lookupFrom(values)); err == nil {
		t.Fatal("expected negative TRUSTED_PROXY_HOPS to fail")
	}
}

func TestLoadFromLookupParsesDefaultsAndRedactsSecrets(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	values := map[string]string{
		"BASE_URL":       "https://zener.example.test",
		"SESSION_SECRET": secret,
		"ADMIN_USERNAME": "root",
		"ADMIN_PASSWORD": "super-secret",
		"DB_PATH":        "/data/zener.db",
		"S3_ENDPOINT":    "https://s3.example.test",
		"S3_REGION":      "eu-central-1",
		"S3_BUCKET":      "zener",
		"S3_ACCESS_KEY":  "access-key",
		"S3_SECRET_KEY":  "secret-key",
		"S3_PREFIX":      "incoming/",
		"S3_PATH_STYLE":  "true",
		"MAX_FILE_SIZE":  "1048576",
		"ALLOWED_EXT":    "pdf, PNG ,zip",
	}

	cfg, err := config.LoadFromLookup(func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	})
	if err != nil {
		t.Fatalf("LoadFromLookup failed: %v", err)
	}

	if cfg.Port != "8080" {
		t.Fatalf("expected default port 8080, got %q", cfg.Port)
	}
	if cfg.AdminUsername != "root" {
		t.Fatalf("unexpected admin username %q", cfg.AdminUsername)
	}
	if cfg.MaxFileSize != 1048576 {
		t.Fatalf("unexpected max file size %d", cfg.MaxFileSize)
	}
	if cfg.TrustedProxyHops != 1 {
		t.Fatalf("expected default TrustedProxyHops 1, got %d", cfg.TrustedProxyHops)
	}
	if got := cfg.AllowedExtensions; len(got) != 3 || got[0] != "pdf" || got[1] != "png" || got[2] != "zip" {
		t.Fatalf("unexpected allowed extensions %#v", got)
	}
	if !cfg.S3.UsePathStyle {
		t.Fatal("expected S3 path-style addressing to be enabled")
	}
	redacted := cfg.Redacted()
	if strings.Contains(redacted, "super-secret") || strings.Contains(redacted, "secret-key") || strings.Contains(redacted, "access-key") {
		t.Fatalf("redacted config leaked a secret: %s", redacted)
	}
}
