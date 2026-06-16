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

export function BrandMotif() {
  return (
    <svg className="brand-motif" aria-hidden="true" focusable="false">
      <defs>
        <pattern id="zener-motif" width="240" height="240" patternUnits="userSpaceOnUse">
          <circle cx="48" cy="56" r="20" />
          <path d="M168 40h40 M188 20v40" />
          <rect x="158" y="150" width="38" height="38" rx="2" />
          <polygon points="64,150 71,170 92,170.5 75,183 81,204 64,192 47,204 53,183 36,170.5 57,170" />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#zener-motif)" />
    </svg>
  );
}
