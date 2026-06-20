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

import { argon2idAsync } from "@noble/hashes/argon2.js";
import { randomBytes } from "@noble/post-quantum/utils.js";
import { parsePrivateIdentity } from "./e2eCrypto";

const DB_NAME = "sprag-e2e-private-keys";
const DB_VERSION = 1;
const STORE_NAME = "privateKeys";
const STORE_RECORD_VERSION = 1;
const STORAGE_ALGORITHM = "ARGON2ID-AES-256-GCM";
const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

export type E2EKeyStoreKDFParams = {
  memoryKiB: number;
  iterations: number;
  parallelism: number;
};

type StoredKDF = E2EKeyStoreKDFParams & {
  name: "argon2id";
  salt: string;
  outputBytes: 32;
};

export type StoredPrivateKeyRecord = {
  id: string;
  version: typeof STORE_RECORD_VERSION;
  pageID: number;
  fingerprint: string;
  algorithm: typeof STORAGE_ALGORITHM;
  kdf: StoredKDF;
  cipher: {
    name: "AES-GCM";
    nonce: string;
    ciphertext: string;
  };
  updatedAt: string;
};

export type BrowserPrivateKeyBackend = {
  get(id: string): Promise<StoredPrivateKeyRecord | undefined>;
  put(record: StoredPrivateKeyRecord): Promise<void>;
};

type StoreOptions = {
  pageID: number;
  fingerprint: string;
  passphrase: string;
  backend?: BrowserPrivateKeyBackend;
};

type SaveOptions = StoreOptions & {
  privateKey: string;
  kdfParams?: E2EKeyStoreKDFParams;
};

const productionKDFParams: E2EKeyStoreKDFParams = {
  memoryKiB: 47104,
  iterations: 3,
  parallelism: 1
};

export async function saveStoredPrivateKey({
  pageID,
  fingerprint,
  privateKey,
  passphrase,
  backend = indexedDBPrivateKeyBackend(),
  kdfParams = productionKDFParams
}: SaveOptions): Promise<void> {
  if (passphrase.length === 0) {
    throw new Error("Passphrase required");
  }
  const identity = parsePrivateIdentity(privateKey);
  if (identity.publicIdentity.fingerprint !== fingerprint) {
    throw new Error("Private key does not match this page");
  }

  const id = storedPrivateKeyID(pageID, fingerprint);
  const kdf: StoredKDF = {
    name: "argon2id",
    salt: bytesToBase64URL(randomBytes(32)),
    outputBytes: 32,
    ...kdfParams
  };
  const nonce = randomBytes(12);
  const baseRecord: Omit<StoredPrivateKeyRecord, "cipher"> = {
    id,
    version: STORE_RECORD_VERSION,
    pageID,
    fingerprint,
    algorithm: STORAGE_ALGORITHM,
    kdf,
    updatedAt: new Date().toISOString()
  };
  const key = await deriveWrappingKey(passphrase, kdf, ["encrypt"]);
  const ciphertext = await crypto.subtle.encrypt(
    {
      name: "AES-GCM",
      iv: bufferSource(nonce),
      additionalData: storageAAD(baseRecord),
      tagLength: 128
    },
    key,
    textEncoder.encode(privateKey)
  );

  await backend.put({
    ...baseRecord,
    cipher: {
      name: "AES-GCM",
      nonce: bytesToBase64URL(nonce),
      ciphertext: bytesToBase64URL(new Uint8Array(ciphertext))
    }
  });
}

export async function loadStoredPrivateKey({
  pageID,
  fingerprint,
  passphrase,
  backend = indexedDBPrivateKeyBackend()
}: StoreOptions): Promise<string | null> {
  if (passphrase.length === 0) {
    throw new Error("Passphrase required");
  }
  const record = await backend.get(storedPrivateKeyID(pageID, fingerprint));
  if (!record) return null;
  validateStoredRecord(record, pageID, fingerprint);

  try {
    const key = await deriveWrappingKey(passphrase, record.kdf, ["decrypt"]);
    const plaintext = await crypto.subtle.decrypt(
      {
        name: "AES-GCM",
        iv: bufferSource(base64URLToBytes(record.cipher.nonce)),
        additionalData: storageAAD(record),
        tagLength: 128
      },
      key,
      bufferSource(base64URLToBytes(record.cipher.ciphertext))
    );
    const privateKey = textDecoder.decode(plaintext);
    const identity = parsePrivateIdentity(privateKey);
    if (identity.publicIdentity.fingerprint !== fingerprint) {
      throw new Error("Private key does not match this page");
    }
    return privateKey;
  } catch {
    throw new Error("Could not decrypt stored private key");
  }
}

export async function storedPrivateKeyExists({
  pageID,
  fingerprint,
  backend = indexedDBPrivateKeyBackend()
}: Omit<StoreOptions, "passphrase">): Promise<boolean> {
  return Boolean(await backend.get(storedPrivateKeyID(pageID, fingerprint)));
}

export function indexedDBPrivateKeyBackend(dbFactory: IDBFactory = indexedDB): BrowserPrivateKeyBackend {
  return {
    async get(id: string) {
      const db = await openPrivateKeyDB(dbFactory);
      try {
        return await requestPromise(db.transaction(STORE_NAME, "readonly").objectStore(STORE_NAME).get(id));
      } finally {
        db.close();
      }
    },
    async put(record: StoredPrivateKeyRecord) {
      const db = await openPrivateKeyDB(dbFactory);
      try {
        const tx = db.transaction(STORE_NAME, "readwrite");
        tx.objectStore(STORE_NAME).put(record);
        await transactionDone(tx);
      } finally {
        db.close();
      }
    }
  };
}

function storedPrivateKeyID(pageID: number, fingerprint: string): string {
  return `${pageID}:${fingerprint}`;
}

async function deriveWrappingKey(passphrase: string, kdf: StoredKDF, keyUsages: KeyUsage[]): Promise<CryptoKey> {
  const keyBytes = await argon2idAsync(passphrase, base64URLToBytes(kdf.salt), {
    t: kdf.iterations,
    m: kdf.memoryKiB,
    p: kdf.parallelism,
    dkLen: kdf.outputBytes,
    maxmem: Math.max(kdf.memoryKiB * 1024 + 1024 * 1024, 64 * 1024 * 1024),
    asyncTick: 20
  });
  try {
    return await crypto.subtle.importKey("raw", bufferSource(keyBytes), { name: "AES-GCM" }, false, keyUsages);
  } finally {
    keyBytes.fill(0);
  }
}

function validateStoredRecord(record: StoredPrivateKeyRecord, pageID: number, fingerprint: string) {
  if (
    record.version !== STORE_RECORD_VERSION ||
    record.pageID !== pageID ||
    record.fingerprint !== fingerprint ||
    record.algorithm !== STORAGE_ALGORITHM ||
    record.kdf.name !== "argon2id" ||
    record.kdf.outputBytes !== 32 ||
    record.cipher.name !== "AES-GCM"
  ) {
    throw new Error("Stored private key metadata is invalid");
  }
}

function storageAAD(record: Omit<StoredPrivateKeyRecord, "cipher"> | StoredPrivateKeyRecord): Uint8Array<ArrayBuffer> {
  return bufferSource(
    textEncoder.encode(
      canonicalJSONString({
        id: record.id,
        version: record.version,
        pageID: record.pageID,
        fingerprint: record.fingerprint,
        algorithm: record.algorithm,
        kdf: record.kdf
      })
    )
  );
}

function openPrivateKeyDB(dbFactory: IDBFactory): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = dbFactory.open(DB_NAME, DB_VERSION);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: "id" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("Could not open browser key store"));
  });
}

function requestPromise<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("IndexedDB request failed"));
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error ?? new Error("IndexedDB transaction aborted"));
    transaction.onerror = () => reject(transaction.error ?? new Error("IndexedDB transaction failed"));
  });
}

function canonicalJSONString(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJSONString).join(",")}]`;
  }
  const record = value as Record<string, unknown>;
  return `{${Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${canonicalJSONString(record[key])}`)
    .join(",")}}`;
}

function bytesToBase64URL(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function base64URLToBytes(value: string): Uint8Array<ArrayBuffer> {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  const binary = atob(padded);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

function bufferSource(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(bytes.byteLength);
  out.set(bytes);
  return out;
}
