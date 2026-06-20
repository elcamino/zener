/*
 * Sprag - a post-quantum-safe end-to-end encrypted file dropbox.
 * Copyright (C) 2026 Tobias von Dewitz <tobias@vondewitz.org>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

import { describe, expect, it } from "vitest";
import { exportPrivateIdentity, generateE2EIdentity } from "./e2eCrypto";
import {
  BrowserPrivateKeyBackend,
  StoredPrivateKeyRecord,
  loadStoredPrivateKey,
  saveStoredPrivateKey,
  storedPrivateKeyExists
} from "./e2eKeyStore";

const testKDFParams = {
  memoryKiB: 1024,
  iterations: 1,
  parallelism: 1
};

class MemoryPrivateKeyBackend implements BrowserPrivateKeyBackend {
  records = new Map<string, StoredPrivateKeyRecord>();

  async get(id: string): Promise<StoredPrivateKeyRecord | undefined> {
    return this.records.get(id);
  }

  async put(record: StoredPrivateKeyRecord): Promise<void> {
    this.records.set(record.id, record);
  }
}

describe("E2E browser private key store", () => {
  it("encrypts the private key record and restores it with the passphrase", async () => {
    const backend = new MemoryPrivateKeyBackend();
    const identity = await generateE2EIdentity();
    const privateKey = exportPrivateIdentity(identity);
    const locator = { pageID: 42, fingerprint: identity.publicIdentity.fingerprint };

    await saveStoredPrivateKey({
      ...locator,
      privateKey,
      passphrase: "correct horse battery staple",
      backend,
      kdfParams: testKDFParams
    });

    const stored = backend.records.values().next().value as StoredPrivateKeyRecord;
    expect(await storedPrivateKeyExists({ ...locator, backend })).toBe(true);
    expect(JSON.stringify(stored)).not.toContain(privateKey);
    expect(JSON.stringify(stored)).not.toContain("secretKey");

    await expect(
      loadStoredPrivateKey({
        ...locator,
        passphrase: "correct horse battery staple",
        backend
      })
    ).resolves.toBe(privateKey);
  });

  it("uses the hardened production Argon2id cost for newly stored keys", async () => {
    const backend = new MemoryPrivateKeyBackend();
    const identity = await generateE2EIdentity();
    const locator = { pageID: 43, fingerprint: identity.publicIdentity.fingerprint };

    await saveStoredPrivateKey({
      ...locator,
      privateKey: exportPrivateIdentity(identity),
      passphrase: "correct horse battery staple",
      backend
    });

    const stored = backend.records.values().next().value as StoredPrivateKeyRecord;
    expect(stored.kdf).toMatchObject({
      name: "argon2id",
      memoryKiB: 47104,
      iterations: 3,
      parallelism: 1,
      outputBytes: 32
    });
  });

  it("does not decrypt the stored private key with the wrong passphrase", async () => {
    const backend = new MemoryPrivateKeyBackend();
    const identity = await generateE2EIdentity();
    const locator = { pageID: 7, fingerprint: identity.publicIdentity.fingerprint };

    await saveStoredPrivateKey({
      ...locator,
      privateKey: exportPrivateIdentity(identity),
      passphrase: "right passphrase",
      backend,
      kdfParams: testKDFParams
    });

    await expect(
      loadStoredPrivateKey({
        ...locator,
        passphrase: "wrong passphrase",
        backend
      })
    ).rejects.toThrow("Could not decrypt stored private key");
  });
});
