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

import { describe, expect, it } from "vitest";
import { createTransferEstimator, formatDuration } from "./transfer";

describe("formatDuration", () => {
  it("formats whole seconds", () => {
    expect(formatDuration(8)).toBe("8s");
  });

  it("rounds to whole seconds", () => {
    expect(formatDuration(8.4)).toBe("8s");
    expect(formatDuration(8.6)).toBe("9s");
  });

  it("formats minutes and seconds", () => {
    expect(formatDuration(80)).toBe("1m 20s");
  });

  it("drops the seconds when they are zero", () => {
    expect(formatDuration(120)).toBe("2m");
  });

  it("formats hours and minutes", () => {
    expect(formatDuration(7500)).toBe("2h 5m");
    expect(formatDuration(7200)).toBe("2h");
  });

  it("returns a dash for unknown durations", () => {
    expect(formatDuration(Number.NaN)).toBe("—");
    expect(formatDuration(Number.POSITIVE_INFINITY)).toBe("—");
    expect(formatDuration(-5)).toBe("—");
  });
});

const MIB = 1024 * 1024;

describe("createTransferEstimator", () => {
  it("returns a null eta on the first sample", () => {
    const estimator = createTransferEstimator();
    const result = estimator.update(0, 10 * MIB, 0);
    expect(result.etaSeconds).toBeNull();
  });

  it("estimates rate and eta from a steady stream", () => {
    const estimator = createTransferEstimator();
    estimator.update(0, 10 * MIB, 0);
    // 1 MiB transferred in 100 ms -> 10 MiB/s.
    const result = estimator.update(1 * MIB, 10 * MIB, 100);
    expect(result.bytesPerSecond).toBeGreaterThan(10 * MIB * 0.99);
    expect(result.bytesPerSecond).toBeLessThan(10 * MIB * 1.01);
    // 9 MiB remaining at 10 MiB/s -> ~0.9 s.
    expect(result.etaSeconds).not.toBeNull();
    expect(result.etaSeconds as number).toBeGreaterThan(0.85);
    expect(result.etaSeconds as number).toBeLessThan(0.95);
  });

  it("smooths toward a new rate when the speed changes", () => {
    const estimator = createTransferEstimator();
    estimator.update(0, 100 * MIB, 0);
    estimator.update(10 * MIB, 100 * MIB, 1000); // establish 10 MiB/s
    // Then only 0.1 MiB in 100 ms -> 1 MiB/s instantaneous.
    const slow = estimator.update(10 * MIB + 0.1 * MIB, 100 * MIB, 1100);
    // EWMA (alpha 0.3): 0.3*1 + 0.7*10 = 7.3 MiB/s -> between 1 and 10 MiB/s.
    expect(slow.bytesPerSecond).toBeGreaterThan(1 * MIB);
    expect(slow.bytesPerSecond).toBeLessThan(10 * MIB);
  });

  it("does not divide by zero on a repeated timestamp", () => {
    const estimator = createTransferEstimator();
    estimator.update(0, 10 * MIB, 0);
    estimator.update(1 * MIB, 10 * MIB, 100);
    const same = estimator.update(2 * MIB, 10 * MIB, 100); // deltaMs == 0
    expect(Number.isFinite(same.bytesPerSecond)).toBe(true);
    expect(same.etaSeconds === null || Number.isFinite(same.etaSeconds)).toBe(true);
  });

  it("reports a zero eta at completion, even as the first sample", () => {
    const estimator = createTransferEstimator();
    const result = estimator.update(10 * MIB, 10 * MIB, 50);
    expect(result.etaSeconds).toBe(0);
  });
});
