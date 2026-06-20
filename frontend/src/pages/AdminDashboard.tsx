// Sprag - a post-quantum-safe end-to-end encrypted file dropbox.
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

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
  Archive,
  Copy,
  Download,
  FileKey2,
  FileDown,
  FileText,
  CircleHelp,
  KeyRound,
  LogOut,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  UploadCloud
} from "lucide-react";
import { api, CreatedPage, E2EConfig, formatBytes, formatDate, PageSummary, ReceiptStatus, UploadFile } from "../api";
import {
  DownloadUnlockPrompt,
  SubmissionFileGroup,
  downloadUnlockPromptActive,
  filesVisibleForSelectedPage,
  groupFilesBySubmission,
  LoadedFiles,
  nextDownloadUnlockPrompt,
  privateKeyControlState,
  receiptStatusHelp,
  receiptStatusLabel,
  sealActionHelp,
  selectedPageForID,
  submitStoredPrivateKeyUnlock
} from "../adminState";
import {
  E2E_ALGORITHM,
  decryptEncryptedUpload,
  exportPrivateIdentity,
  generateE2EIdentity,
  parsePrivateIdentity,
  publicIdentityFromPrivate
} from "../e2eCrypto";
import { loadStoredPrivateKey, saveStoredPrivateKey } from "../e2eKeyStore";
import { ThemeSwitch } from "../ThemeSwitch";

type PageForm = {
  title: string;
  description: string;
  pin: string;
  max_file_size: string;
  allowed_ext: string;
  expires_at: string;
  e2e_enabled: boolean;
};

const emptyForm: PageForm = {
  title: "",
  description: "",
  pin: "",
  max_file_size: "",
  allowed_ext: "",
  expires_at: "",
  e2e_enabled: false
};

const receiptStatuses: ReceiptStatus[] = ["received", "reviewed", "rejected", "downloaded"];

export default function AdminDashboard() {
  const [pages, setPages] = useState<PageSummary[]>([]);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [loadedFiles, setLoadedFiles] = useState<LoadedFiles>(null);
  const [form, setForm] = useState<PageForm>(emptyForm);
  const [created, setCreated] = useState<CreatedPage | null>(null);
  const [e2eConfig, setE2EConfig] = useState<E2EConfig | null>(null);
  const [newPagePrivateKey, setNewPagePrivateKey] = useState("");
  const [newPageKeyCopied, setNewPageKeyCopied] = useState(false);
  const [storeNewPageKey, setStoreNewPageKey] = useState(false);
  const [newPageKeyPassphrase, setNewPageKeyPassphrase] = useState("");
  const [newPageKeyPassphraseConfirm, setNewPageKeyPassphraseConfirm] = useState("");
  const [pagePrivateKeys, setPagePrivateKeys] = useState<Record<number, string>>({});
  const [storedBrowserKeyPassphrases, setStoredBrowserKeyPassphrases] = useState<Record<number, string>>({});
  const [downloadUnlockPrompt, setDownloadUnlockPrompt] = useState<DownloadUnlockPrompt | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const selected = useMemo(() => selectedPageForID(pages, selectedID), [pages, selectedID]);
  const files = useMemo(() => filesVisibleForSelectedPage(loadedFiles, selected), [loadedFiles, selected]);
  const fileGroups = useMemo(() => groupFilesBySubmission(files), [files]);
  const selectedPrivateKey = selected ? (pagePrivateKeys[selected.id] ?? "") : "";
  const selectedStoredKeyControl = privateKeyControlState(selectedPrivateKey);
  const selectedDownloadUnlockPrompt = selected ? downloadUnlockPromptActive(downloadUnlockPrompt, selected.id) : false;

  const loadPages = useCallback(async () => {
    const data = await api<PageSummary[]>("/api/admin/pages");
    setPages(data);
    setSelectedID((current) => {
      if (data.length === 0) return null;
      if (current !== null && data.some((page) => page.id === current)) return current;
      return data[0].id;
    });
  }, []);

  const loadFiles = useCallback(async (pageID: number) => {
    const data = await api<UploadFile[]>(`/api/admin/pages/${pageID}/files`);
    setLoadedFiles({ pageID, files: data });
  }, []);

  useEffect(() => {
    loadPages().catch(() => window.location.assign("/admin"));
  }, [loadPages]);

  useEffect(() => {
    api<E2EConfig>("/api/admin/e2e")
      .then(setE2EConfig)
      .catch((err) => setError(err instanceof Error ? err.message : "Could not load E2E config"));
  }, []);

  useEffect(() => {
    if (selected) {
      loadFiles(selected.id).catch((err) => setError(err instanceof Error ? err.message : "Could not load files"));
    } else {
      setLoadedFiles(null);
    }
  }, [loadFiles, selected]);

  async function createPage(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const e2eSelected = Boolean(e2eConfig?.enabled && (e2eConfig.required || form.e2e_enabled));
      let privateKeyForPage = "";
      let privateKeyFingerprint = "";
      let e2ePayload = {};
      if (e2eSelected) {
        const identity = parsePrivateIdentity(newPagePrivateKey);
        const publicIdentity = publicIdentityFromPrivate(identity);
        if (publicIdentity.algorithm !== (e2eConfig?.algorithm || E2E_ALGORITHM)) {
          throw new Error("Private key algorithm does not match server E2E config");
        }
        if (storeNewPageKey) {
          if (!newPageKeyPassphrase) {
            throw new Error("Passphrase required to store the private key in this browser");
          }
          if (newPageKeyPassphrase !== newPageKeyPassphraseConfirm) {
            throw new Error("Stored key passphrases do not match");
          }
        }
        privateKeyForPage = exportPrivateIdentity(identity);
        privateKeyFingerprint = publicIdentity.fingerprint;
        e2ePayload = {
          e2e_public_key: JSON.stringify(publicIdentity),
          e2e_public_key_fingerprint: publicIdentity.fingerprint,
          e2e_algorithm: publicIdentity.algorithm
        };
      }
      const payload = {
        title: form.title,
        description: form.description,
        pin: form.pin,
        max_file_size: form.max_file_size ? Number(form.max_file_size) : undefined,
        allowed_ext: form.allowed_ext,
        expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : "",
        ...e2ePayload
      };
      const page = await api<CreatedPage>("/api/admin/pages", {
        method: "POST",
        body: JSON.stringify(payload)
      });
      let storageError = "";
      if (privateKeyForPage && storeNewPageKey) {
        try {
          await saveStoredPrivateKey({
            pageID: page.id,
            fingerprint: page.e2e_public_key_fingerprint || privateKeyFingerprint,
            privateKey: privateKeyForPage,
            passphrase: newPageKeyPassphrase
          });
        } catch (err) {
          storageError = err instanceof Error ? err.message : "Could not store private key in this browser";
        }
      }
      setCreated(page);
      setForm(emptyForm);
      setStoreNewPageKey(false);
      setNewPageKeyPassphrase("");
      setNewPageKeyPassphraseConfirm("");
      if (privateKeyForPage) {
        setPagePrivateKeys((current) => ({ ...current, [page.id]: privateKeyForPage }));
      }
      await loadPages();
      setSelectedID(page.id);
      if (storageError) {
        setError(`Page created, but browser key storage failed: ${storageError}`);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create page");
    } finally {
      setBusy(false);
    }
  }

  async function toggleActive(page: PageSummary) {
    await api<PageSummary>(`/api/admin/pages/${page.id}`, {
      method: "PATCH",
      body: JSON.stringify({ is_active: !page.is_active })
    });
    await loadPages();
  }

  async function sealPage(page: PageSummary) {
    if (!window.confirm("Seal this page? Public uploads will close and post-seal admin actions will be recorded.")) return;
    await api<PageSummary>(`/api/admin/pages/${page.id}/seal`, {
      method: "POST",
      body: JSON.stringify({})
    });
    await loadPages();
    setSelectedID(page.id);
  }

  async function deletePage(page: PageSummary, filesToo: boolean) {
    if (!window.confirm(filesToo ? "Delete this page and all files?" : "Delete this page?")) return;
    await api<void>(`/api/admin/pages/${page.id}${filesToo ? "?files=1" : ""}`, { method: "DELETE" });
    setPages((current) => current.filter((candidate) => candidate.id !== page.id));
    setSelectedID(null);
    setCreated(null);
    setLoadedFiles(null);
    setStoredBrowserKeyPassphrases((current) => {
      const next = { ...current };
      delete next[page.id];
      return next;
    });
    await loadPages();
  }

  async function deleteFile(file: UploadFile) {
    if (!selected || !window.confirm(`Delete ${file.name}?`)) return;
    await api<void>(`/api/admin/pages/${selected.id}/files/${file.id}`, { method: "DELETE" });
    await loadFiles(selected.id);
    await loadPages();
  }

  async function updateReceiptStatus(group: SubmissionFileGroup, status: ReceiptStatus) {
    if (!selected) return;
    setError("");
    try {
      await api(`/api/admin/pages/${selected.id}/submissions/${encodeURIComponent(group.submissionID)}/receipt`, {
        method: "PATCH",
        body: JSON.stringify({ status })
      });
      await loadFiles(selected.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not update receipt status");
    }
  }

  async function logout() {
    await api("/api/admin/logout", { method: "POST" });
    window.location.assign("/admin");
  }

  async function generateNewPageKey() {
    setError("");
    setBusy(true);
    try {
      const identity = await generateE2EIdentity();
      setNewPagePrivateKey(exportPrivateIdentity(identity));
      setForm((current) => ({ ...current, e2e_enabled: true }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not generate key");
    } finally {
      setBusy(false);
    }
  }

  async function copyNewPagePrivateKey() {
    if (!newPagePrivateKey) return;
    await navigator.clipboard.writeText(newPagePrivateKey);
    setNewPageKeyCopied(true);
    window.setTimeout(() => setNewPageKeyCopied(false), 1400);
  }

  async function handleNewPageKeyAction() {
    setError("");
    try {
      if (newPagePrivateKey.trim()) {
        await copyNewPagePrivateKey();
        return;
      }
      await generateNewPageKey();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not copy private key");
    }
  }

  async function unlockStoredBrowserKey(page: PageSummary) {
    if (!page.e2e_public_key_fingerprint) return;
    setError("");
    try {
      const passphrase = storedBrowserKeyPassphrases[page.id] ?? "";
      const privateKey = await loadStoredPrivateKey({
        pageID: page.id,
        fingerprint: page.e2e_public_key_fingerprint,
        passphrase
      });
      if (!privateKey) {
        throw new Error("No private key is stored in this browser for this page");
      }
      setPagePrivateKeys((current) => ({ ...current, [page.id]: privateKey }));
      setStoredBrowserKeyPassphrases((current) => ({ ...current, [page.id]: "" }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not unlock stored private key");
    }
  }

  function removePrivateKeyFromMemory(page: PageSummary) {
    setError("");
    setPagePrivateKeys((current) => {
      const next = { ...current };
      delete next[page.id];
      return next;
    });
    setStoredBrowserKeyPassphrases((current) => {
      const next = { ...current };
      delete next[page.id];
      return next;
    });
  }

  async function downloadEncryptedFile(file: UploadFile) {
    if (!selected || !file.encryption_envelope) return;
    setError("");
    try {
      const rawKey = pagePrivateKeys[selected.id] ?? "";
      if (!rawKey.trim()) {
        setDownloadUnlockPrompt((current) => nextDownloadUnlockPrompt(current, selected.id, rawKey));
        return;
      }
      const privateIdentity = parsePrivateIdentity(rawKey);
      const response = await fetch(`/api/admin/pages/${selected.id}/files/${file.id}`, {
        credentials: "include"
      });
      if (!response.ok) {
        throw new Error(`${response.status} ${response.statusText}`);
      }
      const decrypted = await decryptEncryptedUpload(await response.arrayBuffer(), file.encryption_envelope, privateIdentity);
      downloadBlob(decrypted.blob, decrypted.name);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not decrypt file");
    }
  }

  return (
    <main className="admin-shell">
      <header className="admin-topbar">
        <div>
          <p className="eyebrow">Sprag</p>
          <h1>Intake pages</h1>
        </div>
        <div className="topbar-actions">
          <ThemeSwitch />
          <button className="icon-button" onClick={() => loadPages()} title="Refresh">
            <RefreshCw size={18} />
          </button>
          <button className="icon-button" onClick={logout} title="Log out">
            <LogOut size={18} />
          </button>
        </div>
      </header>

      <section className="admin-grid">
        <aside className="panel page-list-panel">
          <form onSubmit={createPage} className="new-page-form">
            <h2>
              <Plus size={18} />
              New page
            </h2>
            <label>
              <span>Title</span>
              <input value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} required />
            </label>
            <label>
              <span>Description</span>
              <textarea value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} />
            </label>
            <div className="field-row">
              <label>
                <span>PIN</span>
                <input value={form.pin} onChange={(event) => setForm({ ...form, pin: event.target.value })} />
              </label>
              <label>
                <span>Max bytes</span>
                <input
                  value={form.max_file_size}
                  onChange={(event) => setForm({ ...form, max_file_size: event.target.value })}
                  inputMode="numeric"
                />
              </label>
            </div>
            <div className="field-row">
              <label>
                <span>Extensions</span>
                <input
                  value={form.allowed_ext}
                  onChange={(event) => setForm({ ...form, allowed_ext: event.target.value })}
                  placeholder="pdf,png,zip"
                />
              </label>
              <label>
                <span>Expires</span>
                <input
                  type="datetime-local"
                  value={form.expires_at}
                  onChange={(event) => setForm({ ...form, expires_at: event.target.value })}
                />
              </label>
            </div>
            {e2eConfig?.enabled && (
              <div className="e2e-page-controls">
                <div className="e2e-choice-row">
                  <label className="check-field">
                    <input
                      type="checkbox"
                      checked={e2eConfig.required || form.e2e_enabled}
                      disabled={e2eConfig.required}
                      onChange={(event) => {
                        setForm({ ...form, e2e_enabled: event.target.checked });
                        if (!event.target.checked) setStoreNewPageKey(false);
                      }}
                    />
                    <span>Encrypt Files</span>
                  </label>
                  {newPagePrivateKey.trim() && (e2eConfig.required || form.e2e_enabled) && (
                    <label className="check-field">
                      <input
                        type="checkbox"
                        checked={storeNewPageKey}
                        onChange={(event) => {
                          setStoreNewPageKey(event.target.checked);
                          if (!event.target.checked) {
                            setNewPageKeyPassphrase("");
                            setNewPageKeyPassphraseConfirm("");
                          }
                        }}
                      />
                      <span>Store encrypted in this browser</span>
                    </label>
                  )}
                </div>
                {(e2eConfig.required || form.e2e_enabled) && (
                  <>
                    <button type="button" className="secondary-action" onClick={handleNewPageKeyAction} disabled={busy}>
                      {newPagePrivateKey.trim() ? <Copy size={17} /> : <KeyRound size={17} />}
                      {newPagePrivateKey.trim() ? (newPageKeyCopied ? "Copied" : "Copy") : "Generate key"}
                    </button>
                    <label>
                      <span>Private key</span>
                      <textarea
                        value={newPagePrivateKey}
                        onChange={(event) => {
                          setNewPagePrivateKey(event.target.value);
                          if (!event.target.value.trim()) {
                            setStoreNewPageKey(false);
                            setNewPageKeyPassphrase("");
                            setNewPageKeyPassphraseConfirm("");
                          }
                        }}
                        spellCheck={false}
                        placeholder="Paste private key JSON"
                      />
                    </label>
                    {storeNewPageKey && (
                      <div className="e2e-store-warning">
                        <p>
                          The private key is encrypted in IndexedDB with this passphrase. This is safer than keeping an
                          unencrypted downloaded key, but weaker than a password manager or offline backup and still
                          exposed to compromised admin-page scripts after unlock.
                        </p>
                        <div className="field-row">
                          <label>
                            <span>Storage passphrase</span>
                            <input
                              type="password"
                              value={newPageKeyPassphrase}
                              onChange={(event) => setNewPageKeyPassphrase(event.target.value)}
                              autoComplete="new-password"
                            />
                          </label>
                          <label>
                            <span>Confirm passphrase</span>
                            <input
                              type="password"
                              value={newPageKeyPassphraseConfirm}
                              onChange={(event) => setNewPageKeyPassphraseConfirm(event.target.value)}
                              autoComplete="new-password"
                            />
                          </label>
                        </div>
                      </div>
                    )}
                  </>
                )}
              </div>
            )}
            {error && <p className="error-line">{error}</p>}
            <button className="primary-action" disabled={busy}>
              <Plus size={18} />
              <span>{busy ? "Creating" : "Create"}</span>
            </button>
          </form>

          <div className="page-list">
            {pages.map((page) => (
              <button
                key={page.id}
                className={`page-row ${selected?.id === page.id ? "selected" : ""}`}
                onClick={() => setSelectedID(page.id)}
              >
                <span className={`status-dot ${page.sealed_at ? "sealed" : page.is_active ? "on" : "off"}`} />
                <span>
                  <strong>{page.title}</strong>
                  <small>
                    {page.upload_count} files · {formatBytes(page.total_bytes)}
                    {page.sealed_at ? " · Sealed" : ""}
                    {page.e2e_enabled ? " · E2E" : ""}
                  </small>
                </span>
              </button>
            ))}
          </div>
        </aside>

        <section className="panel detail-panel">
          {selected ? (
            <>
              <div className="detail-header">
                <div>
                  <p className="eyebrow">{selected.slug}</p>
                  <h2>{selected.title}</h2>
                  {selected.description && <p className="muted">{selected.description}</p>}
                  {selected.sealed_at && <p className="muted">Sealed {formatDate(selected.sealed_at)}</p>}
                </div>
                <div className="detail-actions">
                  {selected.sealed_at ? (
                    <span className="sealed-badge">
                      <ShieldCheck size={17} />
                      Sealed
                    </span>
                  ) : (
                    <>
                      <button className="secondary-action" onClick={() => toggleActive(selected)}>
                        {selected.is_active ? "Deactivate" : "Activate"}
                      </button>
                      <span className="action-tooltip">
                        <button
                          className="secondary-action"
                          onClick={() => sealPage(selected)}
                          aria-describedby={`seal-action-help-${selected.id}`}
                        >
                          <ShieldCheck size={17} />
                          Seal
                        </button>
                        <span className="tooltip-panel" id={`seal-action-help-${selected.id}`} role="tooltip">
                          {sealActionHelp}
                        </span>
                      </span>
                      <button className="icon-button danger" onClick={() => deletePage(selected, false)} title="Delete page">
                        <Trash2 size={18} />
                      </button>
                      <button className="icon-button danger" onClick={() => deletePage(selected, true)} title="Delete page and files">
                        <Archive size={18} />
                      </button>
                    </>
                  )}
                </div>
              </div>

              <ShareBlock page={created?.id === selected.id ? created : null} fallbackSlug={selected.slug} />

              {selected.e2e_enabled && (
                <div className="e2e-key-panel">
                  <div>
                    <p className="eyebrow">Encrypted Files</p>
                    <strong>{selected.e2e_public_key_fingerprint}</strong>
                  </div>
                  {selectedStoredKeyControl === "unlock" && (
                    <div
                      key={`unlock-${selected.id}-${selectedDownloadUnlockPrompt ? downloadUnlockPrompt?.nonce : 0}`}
                      className={`e2e-store-warning ${selectedDownloadUnlockPrompt ? "attention" : ""}`}
                    >
                      {selectedDownloadUnlockPrompt && (
                        <p className="e2e-download-notice" role="status">
                          Downloading this encrypted file requires unlocking the stored key or pasting the private key.
                        </p>
                      )}
                      <p>
                        Enter the stored-key passphrase to unlock the private key for this session; the passphrase is
                        not saved.
                      </p>
                      <form
                        className="e2e-unlock-row"
                        onSubmit={(event) => submitStoredPrivateKeyUnlock(event, selected, unlockStoredBrowserKey)}
                      >
                        <label>
                          <span>Stored key passphrase</span>
                          <input
                            type="password"
                            value={storedBrowserKeyPassphrases[selected.id] ?? ""}
                            onChange={(event) =>
                              setStoredBrowserKeyPassphrases((current) => ({
                                ...current,
                                [selected.id]: event.target.value
                              }))
                            }
                            autoComplete="current-password"
                          />
                        </label>
                        <button type="submit" className="secondary-action">
                          <KeyRound size={17} />
                          Unlock
                        </button>
                      </form>
                    </div>
                  )}
                  {selectedStoredKeyControl === "remove-memory" && (
                    <div className="e2e-store-warning">
                      <p>
                        A private key is loaded for this session. Remove it from memory to require the stored-key
                        passphrase again.
                      </p>
                      <button
                        type="button"
                        className="secondary-action danger e2e-memory-action"
                        onClick={() => removePrivateKeyFromMemory(selected)}
                      >
                        <Trash2 size={17} />
                        Remove Key from Memory
                      </button>
                    </div>
                  )}
                  <label>
                    <span>Private key</span>
                    <textarea
                      value={selectedPrivateKey}
                      onChange={(event) =>
                        setPagePrivateKeys((current) => ({ ...current, [selected.id]: event.target.value }))
                      }
                      spellCheck={false}
                      placeholder="Paste private key JSON"
                    />
                  </label>
                </div>
              )}

              <div className="file-toolbar">
                <h3>
                  <UploadCloud size={18} />
                  Files
                </h3>
                {!selected.e2e_enabled && (
                  <a className="secondary-action" href={`/api/admin/pages/${selected.id}/zip`}>
                    <FileDown size={17} />
                    Zip
                  </a>
                )}
                <a className="secondary-action" href={`/api/admin/pages/${selected.id}/manifest`}>
                  <FileText size={17} />
                  Manifest
                </a>
              </div>

              <div className="file-table">
                {fileGroups.map((group) => (
                  <div className="submission-group" key={group.submissionID}>
                    <div className="submission-header">
                      <span>
                        <strong>Submission {shortSubmissionID(group.submissionID)}</strong>
                        <small>
                          {group.fileCount} {group.fileCount === 1 ? "file" : "files"} · {formatBytes(group.totalBytes)} ·{" "}
                          {formatDate(group.uploadedAt)}
                        </small>
                      </span>
                      <div className="submission-meta-actions">
                        <code>{group.submissionID}</code>
                        <label className="receipt-status-field">
                          <span className="receipt-status-label">
                            {receiptStatusLabel}
                            <span
                              className="help-tooltip"
                              tabIndex={0}
                              aria-label={receiptStatusHelp}
                              aria-describedby={`receipt-status-help-${group.submissionID}`}
                            >
                              <CircleHelp size={14} aria-hidden="true" />
                              <span className="tooltip-panel" id={`receipt-status-help-${group.submissionID}`} role="tooltip">
                                {receiptStatusHelp}
                              </span>
                            </span>
                          </span>
                          <select
                            value={group.receiptStatus ?? "received"}
                            onChange={(event) => void updateReceiptStatus(group, event.target.value as ReceiptStatus)}
                            aria-describedby={`receipt-status-help-${group.submissionID}`}
                          >
                            {receiptStatuses.map((status) => (
                              <option key={status} value={status}>
                                {status}
                              </option>
                            ))}
                          </select>
                        </label>
                        {group.receiptToken && (
                          <a className="secondary-action" href={`/r/${group.receiptToken}`}>
                            Receipt
                          </a>
                        )}
                      </div>
                    </div>
                    <div className="submission-files">
                      {group.files.map((file) => (
                        <div className="file-row" key={file.id}>
                          <span>
                            <strong>{file.encryption_mode === "e2e-v1" ? "Encrypted upload" : file.name}</strong>
                            <small>
                              {formatBytes(file.size)} · {formatDate(file.uploaded_at)}
                              {file.encryption_mode === "e2e-v1" ? " · E2E" : ""}
                            </small>
                          </span>
                          <span className="file-actions">
                            {file.encryption_mode === "e2e-v1" ? (
                              <button className="icon-button" onClick={() => downloadEncryptedFile(file)} title="Decrypt and download">
                                <FileKey2 size={17} />
                              </button>
                            ) : (
                              <a className="icon-button" href={`/api/admin/pages/${selected.id}/files/${file.id}`} title="Download">
                                <Download size={17} />
                              </a>
                            )}
                            <button className="icon-button danger" onClick={() => deleteFile(file)} title="Delete file">
                              <Trash2 size={17} />
                            </button>
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
                {files.length === 0 && <div className="empty-state">No files yet</div>}
              </div>
            </>
          ) : (
            <div className="empty-state">No pages yet</div>
          )}
        </section>
      </section>
    </main>
  );
}

function shortSubmissionID(id: string): string {
  return id.length <= 8 ? id : id.slice(0, 8);
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function ShareBlock({ page, fallbackSlug }: { page: CreatedPage | null; fallbackSlug: string }) {
  const url = page?.url ?? `${window.location.origin}/u/${fallbackSlug}`;
  const [copied, setCopied] = useState(false);

  async function copy() {
    await navigator.clipboard.writeText(url);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  }

  return (
    <div className="share-block">
      <QRCodeSVG value={url} size={112} marginSize={1} />
      <div>
        <p className="eyebrow">Share URL</p>
        <code>{url}</code>
        <button className="secondary-action" onClick={copy}>
          <Copy size={17} />
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
    </div>
  );
}
