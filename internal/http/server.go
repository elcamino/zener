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

package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tob/zener/internal/blob"
	"github.com/tob/zener/internal/e2e"
	"github.com/tob/zener/internal/ids"
	"github.com/tob/zener/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrBlobNotFound = blob.ErrNotFound
	errTooLarge     = errors.New("upload too large")
)

const (
	sessionCookie = "zener_session"
	pinCookie     = "zener_pin"
	csrfHeader    = "X-Zener-CSRF"
)

type Config struct {
	BaseURL           string
	SessionSecret     []byte
	AdminUsername     string
	AdminPassword     string
	AdminPasswordHash string
	MaxFileSize       int64
	AllowedExtensions []string
	S3Prefix          string
	SecureCookies     bool
	TrustedProxyHops  int
	E2EIntake         E2EConfig
}

type E2EConfig struct {
	Enabled   bool
	Required  bool
	Algorithm string
}

type Dependencies struct {
	Store     *store.SQLite
	BlobStore blob.Store
	Config    Config
	Logger    *slog.Logger
	Clock     func() time.Time
	StaticFS  http.FileSystem
}

type Server struct {
	store     *store.SQLite
	blobs     blob.Store
	cfg       Config
	logger    *slog.Logger
	clock     func() time.Time
	passHash  []byte
	loginRate *rateLimiter
	pinRate   *rateLimiter
}

func New(deps Dependencies) (http.Handler, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if deps.BlobStore == nil {
		return nil, fmt.Errorf("blob store is required")
	}
	if len(deps.Config.SessionSecret) < 32 {
		return nil, fmt.Errorf("session secret must be at least 32 bytes")
	}
	if deps.Config.AdminUsername == "" {
		return nil, fmt.Errorf("admin username is required")
	}
	if deps.Config.AdminPassword == "" && deps.Config.AdminPasswordHash == "" {
		return nil, fmt.Errorf("admin password or password hash is required")
	}
	if deps.Config.MaxFileSize <= 0 {
		return nil, fmt.Errorf("max file size must be positive")
	}
	if deps.Config.E2EIntake.Enabled {
		if deps.Config.E2EIntake.Algorithm == "" {
			deps.Config.E2EIntake.Algorithm = e2e.Algorithm
		}
		if !e2e.SupportedAlgorithm(deps.Config.E2EIntake.Algorithm) {
			return nil, fmt.Errorf("unsupported E2E intake algorithm")
		}
	}
	if deps.Config.E2EIntake.Required && !deps.Config.E2EIntake.Enabled {
		return nil, fmt.Errorf("E2E intake cannot be required when disabled")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	// A precomputed hash keeps the plaintext password out of the
	// configuration; fall back to hashing the plaintext at startup.
	var passHash []byte
	if deps.Config.AdminPasswordHash != "" {
		passHash = []byte(deps.Config.AdminPasswordHash)
		if _, err := bcrypt.Cost(passHash); err != nil {
			return nil, fmt.Errorf("invalid admin password hash: %w", err)
		}
	} else {
		hash, err := HashAdminPassword(deps.Config.AdminPassword)
		if err != nil {
			return nil, err
		}
		passHash = []byte(hash)
	}
	s := &Server{
		store:     deps.Store,
		blobs:     deps.BlobStore,
		cfg:       deps.Config,
		logger:    deps.Logger,
		clock:     deps.Clock,
		passHash:  passHash,
		loginRate: newRateLimiter(),
		pinRate:   newRateLimiter(),
	}
	return s.routes(deps.StaticFS), nil
}

func (s *Server) routes(staticFS http.FileSystem) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/api/u/{slug}", s.handlePublicPage)
	r.Post("/api/u/{slug}/pin", s.handlePIN)
	r.Post("/api/u/{slug}", s.handleUpload)

	r.Post("/api/admin/login", s.handleLogin)
	r.Group(func(r chi.Router) {
		r.Use(s.requireAdmin)
		r.Get("/api/admin/me", s.handleMe)
		r.Get("/api/admin/e2e", s.handleE2EConfig)
		r.With(s.requireCSRF).Post("/api/admin/logout", s.handleLogout)
		r.Get("/api/admin/pages", s.handleListPages)
		r.With(s.requireCSRF).Post("/api/admin/pages", s.handleCreatePage)
		r.With(s.requireCSRF).Patch("/api/admin/pages/{pageID}", s.handleUpdatePage)
		r.With(s.requireCSRF).Delete("/api/admin/pages/{pageID}", s.handleDeletePage)
		r.Get("/api/admin/pages/{pageID}/files", s.handleListFiles)
		r.Get("/api/admin/pages/{pageID}/files/{fileID}", s.handleDownloadFile)
		r.With(s.requireCSRF).Delete("/api/admin/pages/{pageID}/files/{fileID}", s.handleDeleteFile)
		r.Get("/api/admin/pages/{pageID}/zip", s.handleZip)
	})

	if staticFS != nil {
		r.NotFound(spaHandler(staticFS))
	}
	return r
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, s.cfg.TrustedProxyHops)
	if !s.loginRate.Allow(ip, 5, time.Minute, s.clock()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	usernameOK := hmac.Equal([]byte(req.Username), []byte(s.cfg.AdminUsername))
	passwordOK := bcrypt.CompareHashAndPassword(s.passHash, prehashSecret(req.Password)) == nil
	if !usernameOK || !passwordOK {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	s.setSignedCookie(w, sessionCookie, s.cfg.AdminUsername, 7*24*time.Hour)
	writeJSON(w, http.StatusOK, map[string]string{"username": s.cfg.AdminUsername})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearCookie(w, sessionCookie, s.cfg.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": s.cfg.AdminUsername})
}

func (s *Server) handleE2EConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   s.cfg.E2EIntake.Enabled,
		"required":  s.cfg.E2EIntake.Required,
		"algorithm": s.cfg.E2EIntake.Algorithm,
	})
}

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.store.ListPages(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pages)
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var req pageRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	page, err := s.createPage(r.Context(), req)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.pageResponse(page))
}

func (s *Server) handleUpdatePage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "pageID")
	if !ok {
		return
	}
	var req pagePatchRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	update, err := s.buildPageUpdate(req)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	page, err := s.store.UpdatePage(r.Context(), pageID, update)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "pageID")
	if !ok {
		return
	}
	if r.URL.Query().Get("files") == "1" {
		uploads, err := s.store.ListUploads(r.Context(), pageID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		for _, upload := range uploads {
			if err := s.blobs.Delete(r.Context(), upload.S3Key); err != nil {
				s.serverError(w, err)
				return
			}
		}
	}
	if err := s.store.DeletePage(r.Context(), pageID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
	} else if err != nil {
		s.serverError(w, err)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "pageID")
	if !ok {
		return
	}
	if _, err := s.store.GetPage(r.Context(), pageID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	uploads, err := s.store.ListUploads(r.Context(), pageID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, uploads)
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "pageID")
	if !ok {
		return
	}
	fileID, ok := parseIDParam(w, r, "fileID")
	if !ok {
		return
	}
	upload, err := s.store.GetUpload(r.Context(), pageID, fileID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	body, err := s.blobs.Download(r.Context(), upload.S3Key)
	if errors.Is(err, blob.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "file object not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(upload.SizeBytes, 10))
	w.Header().Set("Content-Disposition", contentDisposition(upload.OriginalName))
	if _, err := io.Copy(w, body); err != nil {
		if r.Context().Err() != nil {
			// Client disconnected mid-download.
			return
		}
		// The declared Content-Length promises more bytes than we can deliver.
		// Abort the connection so the client detects a failed transfer instead
		// of accepting a silently truncated file.
		s.logger.Error("download stream failed", "upload_id", upload.ID, "error", err)
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "pageID")
	if !ok {
		return
	}
	fileID, ok := parseIDParam(w, r, "fileID")
	if !ok {
		return
	}
	upload, err := s.store.GetUpload(r.Context(), pageID, fileID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.blobs.Delete(r.Context(), upload.S3Key); err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.store.DeleteUpload(r.Context(), pageID, fileID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleZip(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "pageID")
	if !ok {
		return
	}
	page, err := s.store.GetPage(r.Context(), pageID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	uploads, err := s.store.ListUploads(r.Context(), pageID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Verify every object is retrievable before sending any bytes. Once the
	// first zip entry is written the 200 status is committed, so a failure after
	// that point could only produce a truncated archive the client would accept
	// as complete. This costs one extra round-trip per object but keeps the
	// common "object missing" case a clean error instead of a silent partial
	// download.
	for _, upload := range uploads {
		body, err := s.blobs.Download(r.Context(), upload.S3Key)
		if err != nil {
			s.serverError(w, err)
			return
		}
		_ = body.Close()
	}

	filename := safeArchiveName(page.Title)
	if filename == "" {
		filename = page.Slug
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(filename+".zip"))
	zw := zip.NewWriter(w)
	names := map[string]bool{}
	for _, upload := range uploads {
		if err := s.writeZipEntry(r.Context(), zw, names, upload); err != nil {
			if r.Context().Err() != nil {
				// Client disconnected mid-download; nothing left to salvage.
				return
			}
			// Headers are already sent, so the status cannot become an error.
			// Abort the connection without finalizing the central directory so
			// the client sees a broken transfer rather than a "valid" truncated
			// archive.
			s.logger.Error("zip stream failed after headers", "upload_id", upload.ID, "error", err)
			panic(http.ErrAbortHandler)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if err := zw.Close(); err != nil {
		s.logger.Error("zip finalize failed", "page_id", pageID, "error", err)
	}
}

func (s *Server) writeZipEntry(ctx context.Context, zw *zip.Writer, names map[string]bool, upload store.Upload) error {
	body, err := s.blobs.Download(ctx, upload.S3Key)
	if err != nil {
		return err
	}
	defer body.Close()
	name := uniqueZipName(names, upload.OriginalName)
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Store,
	}
	header.SetModTime(upload.UploadedAt)
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, body)
	return err
}

func (s *Server) handlePublicPage(w http.ResponseWriter, r *http.Request) {
	page, ok := s.publicPage(w, r)
	if !ok {
		return
	}
	maxSize := s.effectiveMaxFileSize(page)
	allowed, _ := s.effectiveAllowedExt(page)
	writeJSON(w, http.StatusOK, map[string]any{
		"title":        page.Title,
		"description":  page.Description,
		"pin_required": page.PinHash != "",
		"max_size":     maxSize,
		"allowed_ext":  allowed,
		"e2e":          publicE2E(page),
	})
}

func (s *Server) handlePIN(w http.ResponseWriter, r *http.Request) {
	page, ok := s.publicPage(w, r)
	if !ok {
		return
	}
	if page.PinHash == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	key := page.Slug + ":" + clientIP(r, s.cfg.TrustedProxyHops)
	if !s.pinRate.Allow(key, 10, time.Minute, s.clock()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many PIN attempts")
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(page.PinHash), prehashSecret(req.PIN)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid_pin", "invalid PIN")
		return
	}
	s.setSignedCookie(w, pinCookie, page.Slug, 2*time.Hour)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	page, ok := s.publicPage(w, r)
	if !ok {
		return
	}
	if page.PinHash != "" && !s.validSignedCookie(r, pinCookie, page.Slug) {
		writeError(w, http.StatusForbidden, "pin_required", "PIN required")
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "expected multipart form data")
		return
	}
	uploadParts, err := nextUploadParts(reader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "multipart field 'file' is required")
		return
	}
	part := uploadParts.file
	defer part.Close()
	original := part.FileName()
	if original == "" {
		writeError(w, http.StatusBadRequest, "missing_filename", "uploaded file must have a filename")
		return
	}
	var encryptionMode string
	var encryptionAlgorithm string
	var encryptionEnvelope string
	contentType := part.Header.Get("Content-Type")
	storedName := filepath.Base(original)
	limit := s.effectiveMaxFileSize(page)
	if page.E2EEnabled {
		envelope, err := validateE2EEnvelope(uploadParts.fields["e2e_envelope"], page)
		if err != nil {
			writeRequestError(w, err)
			return
		}
		encryptionMode = e2e.UploadMode
		encryptionAlgorithm = envelope.Algorithm
		encryptionEnvelope = uploadParts.fields["e2e_envelope"]
		contentType = "application/octet-stream"
		limit += e2e.CiphertextOverheadAllowance
	} else {
		if allowed, restricted := s.effectiveAllowedExt(page); restricted && !extensionContains(allowed, original) {
			writeError(w, http.StatusBadRequest, "extension_not_allowed", "this file extension is not allowed")
			return
		}
	}

	uploadID, err := ids.NewUUID()
	if err != nil {
		s.serverError(w, err)
		return
	}
	if page.E2EEnabled {
		storedName = uploadID + ".zener"
	}
	key := s.objectKey(page.Slug, uploadID, storedName)
	counting := &countingLimitReader{r: part, remaining: limit}
	if err := s.blobs.Upload(r.Context(), key, counting, contentType); errors.Is(err, errTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "file exceeds the configured size limit")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	upload, err := s.store.CreateUpload(r.Context(), store.UploadCreate{
		PageID:              page.ID,
		S3Key:               key,
		OriginalName:        storedName,
		SizeBytes:           counting.count,
		ContentType:         contentType,
		UploaderIP:          clientIP(r, s.cfg.TrustedProxyHops),
		EncryptionMode:      encryptionMode,
		EncryptionAlgorithm: encryptionAlgorithm,
		EncryptionEnvelope:  encryptionEnvelope,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":              upload.ID,
		"name":            upload.OriginalName,
		"size":            upload.SizeBytes,
		"encryption_mode": upload.EncryptionMode,
	})
}

func (s *Server) createPage(ctx context.Context, req pageRequest) (store.Page, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return store.Page{}, requestError{status: http.StatusBadRequest, code: "title_required", message: "title is required"}
	}
	maxFileSize, err := s.validateMaxFileSize(req.MaxFileSize)
	if err != nil {
		return store.Page{}, err
	}
	allowed := normalizeExtString(req.AllowedExt)
	expiresAt, err := parseOptionalTime(req.ExpiresAt)
	if err != nil {
		return store.Page{}, err
	}
	pinHash, err := hashOptionalPIN(req.PIN)
	if err != nil {
		return store.Page{}, err
	}
	e2eIdentity, err := s.validatePageE2E(req)
	if err != nil {
		return store.Page{}, err
	}
	for i := 0; i < 8; i++ {
		slug, err := ids.GenerateSlug(24)
		if err != nil {
			return store.Page{}, err
		}
		page, err := s.store.CreatePage(ctx, store.PageCreate{
			Slug:                    slug,
			Title:                   title,
			Description:             strings.TrimSpace(req.Description),
			PinHash:                 pinHash,
			MaxFileSize:             maxFileSize,
			AllowedExt:              allowed,
			ExpiresAt:               expiresAt,
			IsActive:                true,
			E2EEnabled:              e2eIdentity.enabled,
			E2EAlgorithm:            e2eIdentity.algorithm,
			E2EPublicKey:            e2eIdentity.publicKey,
			E2EPublicKeyFingerprint: e2eIdentity.fingerprint,
		})
		if err == nil {
			return page, nil
		}
		if !errors.Is(err, store.ErrDuplicateSlug) {
			return store.Page{}, err
		}
	}
	return store.Page{}, fmt.Errorf("could not generate unique slug")
}

func (s *Server) buildPageUpdate(req pagePatchRequest) (store.PageUpdate, error) {
	var update store.PageUpdate
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return update, requestError{status: http.StatusBadRequest, code: "title_required", message: "title is required"}
		}
		update.Title = &title
	}
	if req.Description.Set {
		desc := strings.TrimSpace(req.Description.Value)
		update.Description = store.NullableString{Set: true, Value: nullableString(desc)}
	}
	if req.PIN.Set {
		hash, err := hashOptionalPIN(req.PIN.Value)
		if err != nil {
			return update, err
		}
		update.PinHash = store.NullableString{Set: true, Value: nullableString(hash)}
	}
	if req.MaxFileSize.Set {
		var ptr *int64
		if req.MaxFileSize.Value != nil {
			valid, err := s.validateMaxFileSize(req.MaxFileSize.Value)
			if err != nil {
				return update, err
			}
			ptr = valid
		}
		update.MaxFileSize = store.NullableInt64{Set: true, Value: ptr}
	}
	if req.AllowedExt.Set {
		allowed := normalizeExtString(req.AllowedExt.Value)
		update.AllowedExt = store.NullableString{Set: true, Value: nullableString(allowed)}
	}
	if req.ExpiresAt.Set {
		expires, err := parseOptionalTime(req.ExpiresAt.Value)
		if err != nil {
			return update, err
		}
		update.ExpiresAt = store.NullableTime{Set: true, Value: expires}
	}
	if req.IsActive != nil {
		update.IsActive = req.IsActive
	}
	return update, nil
}

func (s *Server) publicPage(w http.ResponseWriter, r *http.Request) (store.Page, bool) {
	slug := chi.URLParam(r, "slug")
	page, err := s.store.GetPageBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "upload page not found")
		return store.Page{}, false
	}
	if err != nil {
		s.serverError(w, err)
		return store.Page{}, false
	}
	if !page.IsActive || (page.ExpiresAt != nil && !page.ExpiresAt.After(s.clock())) {
		writeError(w, http.StatusNotFound, "page_closed", "this page is no longer accepting uploads")
		return store.Page{}, false
	}
	return page, true
}

func (s *Server) effectiveMaxFileSize(page store.Page) int64 {
	if page.MaxFileSize != nil && *page.MaxFileSize < s.cfg.MaxFileSize {
		return *page.MaxFileSize
	}
	return s.cfg.MaxFileSize
}

// effectiveAllowedExt returns the extensions an upload may use and whether any
// restriction applies. A non-empty global list is a hard ceiling: a page may
// only narrow within it, never widen past it. When both lists are empty there
// is no restriction at all.
func (s *Server) effectiveAllowedExt(page store.Page) (allowed []string, restricted bool) {
	pageExt := splitExtString(page.AllowedExt)
	global := s.cfg.AllowedExtensions
	switch {
	case len(global) == 0 && len(pageExt) == 0:
		return nil, false
	case len(global) == 0:
		return pageExt, true
	case len(pageExt) == 0:
		return global, true
	default:
		return intersectExt(pageExt, global), true
	}
}

func (s *Server) objectKey(slug, uploadID, original string) string {
	prefix := strings.Trim(s.cfg.S3Prefix, "/")
	name := sanitizeFilename(original)
	parts := []string{slug, uploadID, name}
	if prefix != "" {
		parts = append([]string{prefix}, parts...)
	}
	return strings.Join(parts, "/")
}

func (s *Server) shareURL(slug string) string {
	return strings.TrimRight(s.cfg.BaseURL, "/") + "/u/" + slug
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	s.logger.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

type pageRequest struct {
	Title                   string `json:"title"`
	Description             string `json:"description"`
	PIN                     string `json:"pin"`
	MaxFileSize             *int64 `json:"max_file_size"`
	AllowedExt              string `json:"allowed_ext"`
	ExpiresAt               string `json:"expires_at"`
	E2EPublicKey            string `json:"e2e_public_key"`
	E2EPublicKeyFingerprint string `json:"e2e_public_key_fingerprint"`
	E2EAlgorithm            string `json:"e2e_algorithm"`
}

type pagePatchRequest struct {
	Title       *string     `json:"title"`
	Description patchString `json:"description"`
	PIN         patchString `json:"pin"`
	MaxFileSize patchInt64  `json:"max_file_size"`
	AllowedExt  patchString `json:"allowed_ext"`
	ExpiresAt   patchString `json:"expires_at"`
	IsActive    *bool       `json:"is_active"`
}

type pageResponse struct {
	ID                      int64  `json:"id"`
	Slug                    string `json:"slug"`
	URL                     string `json:"url"`
	Title                   string `json:"title"`
	Description             string `json:"description,omitempty"`
	E2EPublicKey            string `json:"e2e_public_key,omitempty"`
	E2EPublicKeyFingerprint string `json:"e2e_public_key_fingerprint,omitempty"`
	E2EAlgorithm            string `json:"e2e_algorithm,omitempty"`
}

type publicE2EResponse struct {
	Enabled              bool   `json:"enabled"`
	Algorithm            string `json:"algorithm"`
	PublicKey            string `json:"public_key"`
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
}

func (s *Server) pageResponse(page store.Page) pageResponse {
	return pageResponse{
		ID:                      page.ID,
		Slug:                    page.Slug,
		URL:                     s.shareURL(page.Slug),
		Title:                   page.Title,
		Description:             page.Description,
		E2EPublicKey:            page.E2EPublicKey,
		E2EPublicKeyFingerprint: page.E2EPublicKeyFingerprint,
		E2EAlgorithm:            page.E2EAlgorithm,
	}
}

func publicE2E(page store.Page) *publicE2EResponse {
	if !page.E2EEnabled {
		return nil
	}
	return &publicE2EResponse{
		Enabled:              true,
		Algorithm:            page.E2EAlgorithm,
		PublicKey:            page.E2EPublicKey,
		PublicKeyFingerprint: page.E2EPublicKeyFingerprint,
	}
}

type pageE2EIdentity struct {
	enabled     bool
	algorithm   string
	publicKey   string
	fingerprint string
}

func (s *Server) validatePageE2E(req pageRequest) (pageE2EIdentity, error) {
	publicKey := strings.TrimSpace(req.E2EPublicKey)
	fingerprint := strings.TrimSpace(req.E2EPublicKeyFingerprint)
	algorithm := strings.TrimSpace(req.E2EAlgorithm)
	if algorithm == "" {
		algorithm = s.cfg.E2EIntake.Algorithm
	}
	hasAny := publicKey != "" || fingerprint != "" || strings.TrimSpace(req.E2EAlgorithm) != ""
	hasComplete := publicKey != "" && fingerprint != ""
	if s.cfg.E2EIntake.Required && !hasComplete {
		return pageE2EIdentity{}, requestError{status: http.StatusBadRequest, code: "e2e_required", message: "E2E intake requires an encryption public key"}
	}
	if !hasAny {
		return pageE2EIdentity{}, nil
	}
	if !s.cfg.E2EIntake.Enabled {
		return pageE2EIdentity{}, requestError{status: http.StatusBadRequest, code: "e2e_disabled", message: "E2E intake is disabled"}
	}
	if !hasComplete {
		return pageE2EIdentity{}, requestError{status: http.StatusBadRequest, code: "e2e_identity_required", message: "E2E intake requires a public key and fingerprint"}
	}
	if algorithm != s.cfg.E2EIntake.Algorithm || !e2e.SupportedAlgorithm(algorithm) {
		return pageE2EIdentity{}, requestError{status: http.StatusBadRequest, code: "invalid_e2e_algorithm", message: "unsupported E2E intake algorithm"}
	}
	if err := validatePublicIdentity(publicKey, fingerprint, algorithm); err != nil {
		return pageE2EIdentity{}, err
	}
	return pageE2EIdentity{
		enabled:     true,
		algorithm:   algorithm,
		publicKey:   publicKey,
		fingerprint: fingerprint,
	}, nil
}

func validatePublicIdentity(raw, fingerprint, algorithm string) error {
	if len(raw) > 8192 {
		return requestError{status: http.StatusBadRequest, code: "invalid_e2e_public_key", message: "E2E public key is too large"}
	}
	if !strings.HasPrefix(fingerprint, "sha256:") || len(fingerprint) > 128 {
		return requestError{status: http.StatusBadRequest, code: "invalid_e2e_public_key", message: "E2E public key fingerprint is invalid"}
	}
	if strings.Contains(raw, "secretKey") || strings.Contains(raw, "private") {
		return requestError{status: http.StatusBadRequest, code: "invalid_e2e_public_key", message: "E2E public key must not contain private key material"}
	}
	var parsed struct {
		Zener       string `json:"zener"`
		Version     int    `json:"version"`
		Algorithm   string `json:"algorithm"`
		PublicKey   string `json:"publicKey"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return requestError{status: http.StatusBadRequest, code: "invalid_e2e_public_key", message: "E2E public key must be valid JSON"}
	}
	if parsed.Zener != "e2e-public-key" || parsed.Version != 1 || parsed.Algorithm != algorithm || parsed.PublicKey == "" || parsed.Fingerprint != fingerprint {
		return requestError{status: http.StatusBadRequest, code: "invalid_e2e_public_key", message: "E2E public key does not match the requested algorithm and fingerprint"}
	}
	return nil
}

type e2eEnvelope struct {
	Version                int    `json:"version"`
	Algorithm              string `json:"algorithm"`
	PublicKeyFingerprint   string `json:"public_key_fingerprint"`
	KEMCiphertext          string `json:"kem_ciphertext"`
	ECDHEphemeralPublicKey string `json:"ecdh_ephemeral_public_key"`
	Salt                   string `json:"salt"`
	FileNonce              string `json:"file_nonce"`
	MetadataNonce          string `json:"metadata_nonce"`
	EncryptedMetadata      string `json:"encrypted_metadata"`
}

func validateE2EEnvelope(raw string, page store.Page) (e2eEnvelope, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return e2eEnvelope{}, requestError{status: http.StatusBadRequest, code: "e2e_required", message: "encrypted pages require an E2E envelope"}
	}
	if len(raw) > 65536 {
		return e2eEnvelope{}, requestError{status: http.StatusBadRequest, code: "invalid_e2e_envelope", message: "E2E envelope is too large"}
	}
	var envelope e2eEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return e2eEnvelope{}, requestError{status: http.StatusBadRequest, code: "invalid_e2e_envelope", message: "E2E envelope must be valid JSON"}
	}
	if envelope.Version != 1 || envelope.Algorithm != page.E2EAlgorithm || envelope.PublicKeyFingerprint != page.E2EPublicKeyFingerprint {
		return e2eEnvelope{}, requestError{status: http.StatusBadRequest, code: "invalid_e2e_envelope", message: "E2E envelope does not match this page"}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"kem_ciphertext", envelope.KEMCiphertext},
		{"ecdh_ephemeral_public_key", envelope.ECDHEphemeralPublicKey},
		{"salt", envelope.Salt},
		{"file_nonce", envelope.FileNonce},
		{"metadata_nonce", envelope.MetadataNonce},
		{"encrypted_metadata", envelope.EncryptedMetadata},
	} {
		if field.value == "" {
			return e2eEnvelope{}, requestError{status: http.StatusBadRequest, code: "invalid_e2e_envelope", message: "E2E envelope is missing " + field.name}
		}
		if _, err := base64.RawURLEncoding.DecodeString(field.value); err != nil {
			return e2eEnvelope{}, requestError{status: http.StatusBadRequest, code: "invalid_e2e_envelope", message: "E2E envelope contains invalid base64url"}
		}
	}
	return envelope, nil
}

type patchString struct {
	Set   bool
	Value string
}

func (p *patchString) UnmarshalJSON(data []byte) error {
	p.Set = true
	if string(data) == "null" {
		p.Value = ""
		return nil
	}
	return json.Unmarshal(data, &p.Value)
}

type patchInt64 struct {
	Set   bool
	Value *int64
}

func (p *patchInt64) UnmarshalJSON(data []byte) error {
	p.Set = true
	if string(data) == "null" {
		p.Value = nil
		return nil
	}
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Value = &value
	return nil
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	return true
}

type requestError struct {
	status  int
	code    string
	message string
}

func (e requestError) Error() string {
	return e.message
}

func writeRequestError(w http.ResponseWriter, err error) {
	var reqErr requestError
	if errors.As(err, &reqErr) {
		writeError(w, reqErr.status, reqErr.code, reqErr.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func parseIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.validSignedCookie(r, sessionCookie, s.cfg.AdminUsername) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "admin session required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(csrfHeader) == "" {
			writeError(w, http.StatusForbidden, "csrf_required", "admin mutations require CSRF header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSignedCookie(w http.ResponseWriter, name, subject string, ttl time.Duration) {
	expires := s.clock().Add(ttl).Unix()
	payload := fmt.Sprintf("%s|%d", subject, expires)
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sign(payload, s.cfg.SessionSecret)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  time.Unix(expires, 0),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) validSignedCookie(r *http.Request, name, wantSubject string) bool {
	cookie, err := r.Cookie(name)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := string(payloadBytes)
	if !hmac.Equal([]byte(parts[1]), []byte(sign(payload, s.cfg.SessionSecret))) {
		return false
	}
	subject, expiresText, ok := strings.Cut(payload, "|")
	if !ok || subject != wantSubject {
		return false
	}
	expires, err := strconv.ParseInt(expiresText, 10, 64)
	if err != nil {
		return false
	}
	return s.clock().Before(time.Unix(expires, 0))
}

func sign(payload string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

type rateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]rateBucket
	lastSweep time.Time
}

type rateBucket struct {
	start time.Time
	count int
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]rateBucket{}}
}

func (r *rateLimiter) Allow(key string, limit int, window time.Duration, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Without eviction the map grows unbounded as new keys (IPs, slug+IP pairs)
	// arrive. Sweep at most once per window, dropping buckets whose window has
	// already elapsed — those would be reset on next access anyway.
	if now.Sub(r.lastSweep) >= window {
		for k, b := range r.buckets {
			if now.Sub(b.start) >= window {
				delete(r.buckets, k)
			}
		}
		r.lastSweep = now
	}
	bucket := r.buckets[key]
	if bucket.start.IsZero() || now.Sub(bucket.start) >= window {
		r.buckets[key] = rateBucket{start: now, count: 1}
		return true
	}
	if bucket.count >= limit {
		return false
	}
	bucket.count++
	r.buckets[key] = bucket
	return true
}

// clientIP resolves the originating client address used for rate-limiting keys
// and audit logging. X-Forwarded-For is attacker-controlled on its left side:
// each trusted proxy appends the peer it observed to the right, so only the
// rightmost trustedHops entries are trustworthy. With trustedHops == 0 the
// header is ignored entirely and the direct TCP peer is used; this is the safe
// default for a directly exposed server. Behind a single proxy (Caddy) set
// trustedHops to 1 so the entry Caddy appended wins over any spoofed prefix.
func clientIP(r *http.Request, trustedHops int) string {
	if trustedHops > 0 {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			idx := len(parts) - trustedHops
			if idx < 0 {
				idx = 0
			}
			if ip := strings.TrimSpace(parts[idx]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type countingLimitReader struct {
	r         io.Reader
	remaining int64
	count     int64
}

func (r *countingLimitReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, errTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	r.count += int64(n)
	return n, err
}

type uploadParts struct {
	fields map[string]string
	file   *multipart.Part
}

func nextUploadParts(reader *multipart.Reader) (uploadParts, error) {
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err != nil {
			return uploadParts{}, err
		}
		if part.FormName() == "file" {
			return uploadParts{fields: fields, file: part}, nil
		}
		if part.FileName() == "" && part.FormName() != "" {
			value, err := readSmallMultipartField(part)
			_ = part.Close()
			if err != nil {
				return uploadParts{}, err
			}
			fields[part.FormName()] = value
			continue
		}
		_ = part.Close()
	}
}

func readSmallMultipartField(part *multipart.Part) (string, error) {
	const maxFieldBytes = 64 << 10
	var buf bytes.Buffer
	n, err := io.CopyN(&buf, part, maxFieldBytes+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n > maxFieldBytes {
		return "", fmt.Errorf("multipart field too large")
	}
	return buf.String(), nil
}

// extensionContains reports whether filename's extension is in allowed. Unlike a
// "no restriction" check, an empty list matches nothing, so a deny-all result
// from an empty page-vs-global intersection rejects every upload.
func extensionContains(allowed []string, filename string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	for _, candidate := range allowed {
		if ext == candidate {
			return true
		}
	}
	return false
}

func intersectExt(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, ext := range b {
		set[ext] = true
	}
	var out []string
	for _, ext := range a {
		if set[ext] {
			out = append(out, ext)
		}
	}
	return out
}

func normalizeExtString(raw string) string {
	return strings.Join(splitExtString(raw), ",")
}

func splitExtString(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(item)), ".")
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		out = append(out, ext)
	}
	return out
}

func (s *Server) validateMaxFileSize(v *int64) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	if *v <= 0 {
		return nil, requestError{status: http.StatusBadRequest, code: "invalid_max_file_size", message: "max_file_size must be positive"}
	}
	if *v > s.cfg.MaxFileSize {
		return nil, requestError{status: http.StatusBadRequest, code: "invalid_max_file_size", message: "max_file_size cannot exceed the global limit"}
	}
	return v, nil
}

func hashOptionalPIN(pin string) (string, error) {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword(prehashSecret(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// HashAdminPassword returns a bcrypt hash of the admin password that is
// compatible with login verification. Store the result in ADMIN_PASSWORD_HASH
// to keep the plaintext password out of the configuration.
func HashAdminPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(prehashSecret(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// prehashSecret hashes a secret to a fixed-length, NUL-free token before bcrypt.
// bcrypt only considers the first 72 bytes and stops at a NUL, so feeding it the
// base64 of a SHA-256 digest lets passwords and PINs of any length and content
// be compared in full.
func prehashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return []byte(base64.RawStdEncoding.EncodeToString(sum[:]))
}

func parseOptionalTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, requestError{status: http.StatusBadRequest, code: "invalid_expires_at", message: "expires_at must be an RFC3339 timestamp"}
	}
	return &parsed, nil
}

func nullableString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return "file"
	}
	return name
}

// contentDisposition builds an attachment header that preserves a non-ASCII
// original filename. It emits a sanitized ASCII fallback (filename=) for legacy
// clients plus the RFC 5987 extended form (filename*=) carrying the UTF-8 name,
// which mime.FormatMediaType does not produce.
func contentDisposition(name string) string {
	name = sanitizeFilename(name)
	var ascii strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 32 || c > 126 || c == '"' || c == '\\' {
			ascii.WriteByte('_')
		} else {
			ascii.WriteByte(c)
		}
	}
	fallback := strings.TrimSpace(ascii.String())
	if fallback == "" {
		fallback = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallback, rfc5987Encode(name))
}

// rfc5987Encode percent-encodes a UTF-8 string per RFC 5987 ext-value rules,
// leaving only attr-char bytes literal.
func rfc5987Encode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

func safeArchiveName(name string) string {
	name = sanitizeFilename(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ToLower(name)
	return strings.Trim(name, ".-_")
}

// uniqueZipName returns a name not yet present in used and records it. When the
// sanitized name is taken it appends -2, -3, ... and re-checks each candidate
// against used, so a generated suffix can never collide with another entry's
// real name.
func uniqueZipName(used map[string]bool, original string) string {
	name := sanitizeFilename(original)
	if !used[name] {
		used[name] = true
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d%s", base, n, ext)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}
