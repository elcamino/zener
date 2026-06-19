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

import { useEffect, useMemo, useState } from "react";
import { CheckCircle2, CircleDot, FileCheck2, Hash, XCircle } from "lucide-react";
import { api, formatBytes, formatDate, PublicReceipt, ReceiptStatus } from "../api";

const receiptLabels: Record<ReceiptStatus, string> = {
  received: "Received",
  reviewed: "Reviewed",
  rejected: "Rejected",
  downloaded: "Downloaded"
};

export default function Receipt() {
  const token = useMemo(() => window.location.pathname.split("/").filter(Boolean)[1] ?? "", []);
  const [receipt, setReceipt] = useState<PublicReceipt | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api<PublicReceipt>(`/api/r/${encodeURIComponent(token)}`)
      .then(setReceipt)
      .catch((err) => setError(err instanceof Error ? err.message : "Receipt not found"));
  }, [token]);

  if (error) {
    return (
      <main className="upload-shell">
        <section className="closed-panel">
          <XCircle size={28} />
          <h1>{error}</h1>
        </section>
      </main>
    );
  }

  if (!receipt) {
    return <main className="route-loading">Sprag</main>;
  }

  return <ReceiptView receipt={receipt} token={token} />;
}

type ReceiptViewProps = {
  receipt: PublicReceipt;
  token: string;
};

export function ReceiptView({ receipt, token }: ReceiptViewProps) {
  return (
    <main className="upload-shell">
      <section className="receipt-panel">
        <div className="upload-heading">
          <span className={`mark receipt-status-mark ${receipt.status}`}>
            {receipt.status === "received" ? <CheckCircle2 size={22} /> : <CircleDot size={22} />}
          </span>
          <div>
            <p className="eyebrow">Sprag receipt</p>
            <h1>Submission {receiptLabels[receipt.status].toLowerCase()}</h1>
            <p className="muted">{formatDate(receipt.submitted_at)}</p>
          </div>
        </div>

        <div className="receipt-facts">
          <div>
            <FileCheck2 size={20} />
            <span>
              <strong>
                {receipt.file_count} {receipt.file_count === 1 ? "file" : "files"}
              </strong>
              <small>{formatBytes(receipt.total_size)}</small>
            </span>
          </div>
          <div>
            <CircleDot size={20} />
            <span>
              <strong>{receiptLabels[receipt.status]}</strong>
              <small>{formatDate(receipt.updated_at)}</small>
            </span>
          </div>
          <div className="receipt-id-fact">
            <Hash size={20} />
            <span>
              <strong>Receipt ID</strong>
              <small className="receipt-id-value">{token}</small>
            </span>
          </div>
        </div>
      </section>
    </main>
  );
}
