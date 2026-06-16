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

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "—";
  }
  const total = Math.round(seconds);
  if (total < 60) {
    return `${total}s`;
  }
  if (total < 3600) {
    const minutes = Math.floor(total / 60);
    const rest = total % 60;
    return rest === 0 ? `${minutes}m` : `${minutes}m ${rest}s`;
  }
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
}

const SMOOTHING = 0.3;

export type TransferSample = {
  bytesPerSecond: number;
  etaSeconds: number | null;
};

export type TransferEstimator = {
  update(loaded: number, total: number, nowMs: number): TransferSample;
};

export function createTransferEstimator(): TransferEstimator {
  let lastMs: number | null = null;
  let lastLoaded = 0;
  let rate: number | null = null; // bytes/second, exponentially smoothed

  return {
    update(loaded: number, total: number, nowMs: number): TransferSample {
      // 1. Completion takes precedence, even on the very first sample.
      if (loaded >= total) {
        return { bytesPerSecond: rate ?? 0, etaSeconds: 0 };
      }
      // 2. First sample: no interval yet, so no rate can be derived.
      if (lastMs === null) {
        lastMs = nowMs;
        lastLoaded = loaded;
        return { bytesPerSecond: rate ?? 0, etaSeconds: null };
      }
      const deltaMs = nowMs - lastMs;
      // 3. Guard against a non-positive interval (two events on the same clock).
      if (deltaMs <= 0) {
        const eta = rate && rate > 0 ? (total - loaded) / rate : null;
        return { bytesPerSecond: rate ?? 0, etaSeconds: eta };
      }
      const instant = ((loaded - lastLoaded) / deltaMs) * 1000;
      rate = rate === null ? instant : SMOOTHING * instant + (1 - SMOOTHING) * rate;
      lastMs = nowMs;
      lastLoaded = loaded;
      const eta = rate > 0 ? (total - loaded) / rate : null;
      return { bytesPerSecond: rate, etaSeconds: eta };
    }
  };
}
