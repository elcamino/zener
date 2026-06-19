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

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ReceiptView } from "./Receipt";

describe("ReceiptView", () => {
  it("shows the receipt ID from the receipt URL", () => {
    const html = renderToStaticMarkup(
      <ReceiptView
        token="receipt-token-123"
        receipt={{
          status: "received",
          submitted_at: "2026-06-19T10:00:00Z",
          updated_at: "2026-06-19T10:00:00Z",
          file_count: 1,
          total_size: 512
        }}
      />
    );

    expect(html).toContain("Receipt ID");
    expect(html).toContain("receipt-token-123");
  });
});
