//ModeToggle

import React from "react";
import { Moon, Sun, Monitor } from "lucide-react";
import { useTheme } from "@/components/theme-provider";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

export function ModeToggle() {
  const { theme, setTheme } = useTheme();

  return (
    <div>
      <ToggleGroup type="single" value={theme} onValueChange={setTheme}>
        <ToggleGroupItem value="light" aria-label="Light mode" className="p-2">
          <Sun className="h-5 w-5" />
          <span className="hidden sm:inline ml-2 text-xs">Light</span>
        </ToggleGroupItem>
        <ToggleGroupItem value="dark" aria-label="Dark mode" className="p-2">
          <Moon className="h-5 w-5" />
          <span className="hidden sm:inline ml-2 text-xs">Dark</span>
        </ToggleGroupItem>
        <ToggleGroupItem
          value="system"
          aria-label="System theme"
          className="p-2"
        >
          <Monitor className="h-5 w-5" />{" "}
          <span className="hidden sm:inline ml-2 text-xs">System</span>
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
  );
}
