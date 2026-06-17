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

package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/tob/zener/internal/e2e"
)

const defaultMaxFileSize int64 = 5 * 1024 * 1024 * 1024

type Config struct {
	Port              string    `json:"port"`
	BaseURL           string    `json:"base_url"`
	SessionSecret     []byte    `json:"-"`
	AdminUsername     string    `json:"admin_username"`
	AdminPassword     string    `json:"-"`
	AdminPasswordHash string    `json:"-"`
	MaxFileSize       int64     `json:"max_file_size"`
	AllowedExtensions []string  `json:"allowed_ext,omitempty"`
	DBPath            string    `json:"db_path"`
	TrustedProxyHops  int       `json:"trusted_proxy_hops"`
	E2EIntake         E2EConfig `json:"e2e_intake"`
	S3                S3Config  `json:"s3"`
}

type E2EConfig struct {
	Enabled   bool   `json:"enabled"`
	Required  bool   `json:"required"`
	Algorithm string `json:"algorithm"`
}

type S3Config struct {
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	AccessKey    string `json:"-"`
	SecretKey    string `json:"-"`
	UsePathStyle bool   `json:"use_path_style"`
	Prefix       string `json:"prefix"`
}

func Load() (Config, error) {
	_ = godotenv.Load()
	return LoadFromLookup(os.LookupEnv)
}

func LoadFromLookup(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}

	var missing []string
	require := func(key string) string {
		v := get(key, "")
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	cfg := Config{
		Port:              get("PORT", "8080"),
		BaseURL:           strings.TrimRight(require("BASE_URL"), "/"),
		AdminUsername:     get("ADMIN_USERNAME", "admin"),
		AdminPassword:     get("ADMIN_PASSWORD", ""),
		AdminPasswordHash: get("ADMIN_PASSWORD_HASH", ""),
		DBPath:            get("DB_PATH", "/data/zener.db"),
		S3: S3Config{
			Endpoint:  require("S3_ENDPOINT"),
			Region:    require("S3_REGION"),
			Bucket:    require("S3_BUCKET"),
			AccessKey: require("S3_ACCESS_KEY"),
			SecretKey: require("S3_SECRET_KEY"),
			Prefix:    get("S3_PREFIX", "pages/"),
		},
	}

	secret := require("SESSION_SECRET")
	if secret != "" {
		decoded, err := base64.StdEncoding.DecodeString(secret)
		if err != nil {
			missing = append(missing, "SESSION_SECRET(base64)")
		} else if len(decoded) < 32 {
			missing = append(missing, "SESSION_SECRET(>=32 bytes)")
		} else {
			cfg.SessionSecret = decoded
		}
	}

	maxFileSize := get("MAX_FILE_SIZE", strconv.FormatInt(defaultMaxFileSize, 10))
	parsedMax, err := strconv.ParseInt(maxFileSize, 10, 64)
	if err != nil || parsedMax <= 0 {
		return Config{}, fmt.Errorf("MAX_FILE_SIZE must be a positive integer")
	}
	cfg.MaxFileSize = parsedMax
	cfg.AllowedExtensions = parseExtList(get("ALLOWED_EXT", ""))

	hops, err := strconv.Atoi(get("TRUSTED_PROXY_HOPS", "1"))
	if err != nil || hops < 0 {
		return Config{}, fmt.Errorf("TRUSTED_PROXY_HOPS must be a non-negative integer")
	}
	cfg.TrustedProxyHops = hops

	e2eEnabled, err := strconv.ParseBool(get("E2E_INTAKE_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("E2E_INTAKE_ENABLED must be a boolean")
	}
	e2eRequired, err := strconv.ParseBool(get("E2E_INTAKE_REQUIRED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("E2E_INTAKE_REQUIRED must be a boolean")
	}
	if e2eRequired && !e2eEnabled {
		return Config{}, fmt.Errorf("E2E_INTAKE_REQUIRED cannot be true when E2E_INTAKE_ENABLED is false")
	}
	e2eAlgorithm := get("E2E_INTAKE_ALGORITHM", e2e.Algorithm)
	if !e2e.SupportedAlgorithm(e2eAlgorithm) {
		return Config{}, fmt.Errorf("E2E_INTAKE_ALGORITHM must be %s", e2e.Algorithm)
	}
	cfg.E2EIntake = E2EConfig{
		Enabled:   e2eEnabled,
		Required:  e2eRequired,
		Algorithm: e2eAlgorithm,
	}

	pathStyle := get("S3_USE_PATH_STYLE", "")
	if pathStyle == "" {
		pathStyle = get("S3_PATH_STYLE", "false")
	}
	usePathStyle, err := strconv.ParseBool(pathStyle)
	if err != nil {
		return Config{}, fmt.Errorf("S3_USE_PATH_STYLE must be a boolean")
	}
	cfg.S3.UsePathStyle = usePathStyle

	if cfg.AdminPassword == "" && cfg.AdminPasswordHash == "" {
		missing = append(missing, "ADMIN_PASSWORD or ADMIN_PASSWORD_HASH")
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing or invalid required config: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func (c Config) Redacted() string {
	type redactedConfig Config
	out := struct {
		redactedConfig
		SessionSecret     string `json:"session_secret"`
		AdminPassword     string `json:"admin_password"`
		AdminPasswordHash string `json:"admin_password_hash"`
		S3AccessKey       string `json:"s3_access_key"`
		S3SecretKey       string `json:"s3_secret_key"`
	}{
		redactedConfig:    redactedConfig(c),
		SessionSecret:     redact(c.SessionSecret != nil),
		AdminPassword:     redact(c.AdminPassword != ""),
		AdminPasswordHash: redact(c.AdminPasswordHash != ""),
		S3AccessKey:       redact(c.S3.AccessKey != ""),
		S3SecretKey:       redact(c.S3.SecretKey != ""),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseExtList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(part)), ".")
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		out = append(out, ext)
	}
	return out
}

func redact(ok bool) string {
	if !ok {
		return ""
	}
	return "[redacted]"
}

func IsMissingConfig(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "missing or invalid required config") || errors.Is(err, os.ErrInvalid))
}
