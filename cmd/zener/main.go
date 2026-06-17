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

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	zener "github.com/tob/zener"
	"github.com/tob/zener/internal/config"
	httpapi "github.com/tob/zener/internal/http"
	s3store "github.com/tob/zener/internal/s3"
	"github.com/tob/zener/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		if err := hashPassword(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(logger); err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

// hashPassword prints a bcrypt hash for an admin password, suitable for the
// ADMIN_PASSWORD_HASH configuration value. The password is read from the first
// argument or, if none is given, from stdin (so it can be piped in).
func hashPassword(args []string) error {
	var password string
	if len(args) > 0 {
		password = args[0]
	} else {
		fmt.Fprint(os.Stderr, "Enter admin password: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return fmt.Errorf("no password provided")
		}
		password = scanner.Text()
	}
	password = strings.TrimRight(password, "\r\n")
	if password == "" {
		return fmt.Errorf("password must not be empty")
	}
	hash, err := httpapi.HashAdminPassword(password)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Info("configuration loaded", "config", cfg.Redacted())

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	objects, err := s3store.New(ctx, s3store.Config{
		Endpoint:     cfg.S3.Endpoint,
		Region:       cfg.S3.Region,
		Bucket:       cfg.S3.Bucket,
		AccessKey:    cfg.S3.AccessKey,
		SecretKey:    cfg.S3.SecretKey,
		UsePathStyle: cfg.S3.UsePathStyle,
	})
	if err != nil {
		return err
	}

	frontend, err := zener.FrontendFS()
	if err != nil {
		return err
	}
	handler, err := httpapi.New(httpapi.Dependencies{
		Store:     db,
		BlobStore: objects,
		Config: httpapi.Config{
			BaseURL:           cfg.BaseURL,
			SessionSecret:     cfg.SessionSecret,
			AdminUsername:     cfg.AdminUsername,
			AdminPassword:     cfg.AdminPassword,
			AdminPasswordHash: cfg.AdminPasswordHash,
			MaxFileSize:       cfg.MaxFileSize,
			AllowedExtensions: cfg.AllowedExtensions,
			S3Prefix:          cfg.S3.Prefix,
			SecureCookies:     true,
			TrustedProxyHops:  cfg.TrustedProxyHops,
			E2EIntake: httpapi.E2EConfig{
				Enabled:   cfg.E2EIntake.Enabled,
				Required:  cfg.E2EIntake.Required,
				Algorithm: cfg.E2EIntake.Algorithm,
			},
		},
		Logger:   logger,
		StaticFS: httpapi.FS(frontend),
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
