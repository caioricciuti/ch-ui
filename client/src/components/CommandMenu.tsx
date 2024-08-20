import { useEffect, useState } from "react";
import {
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandSeparator,
} from "@/components/ui/command";
import { useNavigate } from "react-router-dom";
import useAuthStore from "@/stores/user.store";
import { useTheme } from "@/components/theme-provider";

export function CommandMenu() {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const { logout } = useAuthStore();
  const { setTheme } = useTheme();

  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((open) => !open);
      }
    };

    document.addEventListener("keydown", down);
    return () => document.removeEventListener("keydown", down);
  }, []);

  const runCommand = (command: () => void) => {
    setOpen(false);
    command();
  };

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
        <CommandGroup heading="Navigation">
          <CommandItem
            onSelect={() => runCommand(() => navigate("/credentials"))}
          >
            Credentials
          </CommandItem>
          <CommandItem
            onSelect={() => runCommand(() => navigate("/organizations"))}
          >
            Organizations
          </CommandItem>
          <CommandItem
            onSelect={() => runCommand(() => navigate("/workspace"))}
          >
            Worskpace
          </CommandItem>
          <CommandItem onSelect={() => runCommand(() => navigate("/settings"))}>
            Settings
          </CommandItem>
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading="Actions">
          <CommandItem onSelect={() => runCommand(() => setTheme("dark"))}>
            Dark Mode
          </CommandItem>
          <CommandItem onSelect={() => runCommand(() => setTheme("light"))}>
            Light Mode
          </CommandItem>
          <CommandItem onSelect={async () => await logout()}>
            Logout
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
