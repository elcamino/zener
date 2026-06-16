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

import { useSyncExternalStore } from "react";
import { Monitor, Moon, Sun } from "lucide-react";
import { createThemeController, type ThemeMode } from "./theme";

const controller = createThemeController();

const OPTIONS: { mode: ThemeMode; label: string; Icon: typeof Monitor }[] = [
  { mode: "system", label: "System theme", Icon: Monitor },
  { mode: "light", label: "Light theme", Icon: Sun },
  { mode: "dark", label: "Dark theme", Icon: Moon }
];

export function ThemeSwitch({ fixed = false }: { fixed?: boolean }) {
  const mode = useSyncExternalStore(controller.subscribe, controller.getMode, controller.getMode);

  return (
    <div className={fixed ? "theme-switch theme-switch-fixed" : "theme-switch"} role="group" aria-label="Theme">
      {OPTIONS.map(({ mode: optionMode, label, Icon }) => (
        <button
          key={optionMode}
          type="button"
          aria-label={label}
          aria-pressed={mode === optionMode}
          onClick={() => controller.setMode(optionMode)}
        >
          <Icon size={16} aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}
