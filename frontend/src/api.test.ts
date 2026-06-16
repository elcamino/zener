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
import { formatBytes } from "./api";

describe("formatBytes", () => {
  it("formats bytes below 1 KiB", () => {
    expect(formatBytes(512)).toBe("512 B");
  });

  it("formats kibibytes with two decimals below ten", () => {
    expect(formatBytes(1536)).toBe("1.50 KiB");
  });

  it("formats mebibytes with one decimal at or above ten", () => {
    expect(formatBytes(50 * 1024 * 1024)).toBe("50.0 MiB");
  });

  it("formats gibibytes", () => {
    expect(formatBytes(3 * 1024 * 1024 * 1024)).toBe("3.00 GiB");
  });
});
