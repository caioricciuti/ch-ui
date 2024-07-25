import * as React from "react";
import { Check, ChevronsUpDown, Key } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import useAuthStore from "@/stores/user.store";
import { toast } from "sonner";

export function CredentialSelector() {
  const {
    credentials,
    fetchCredentials,
    isLoading: isCredentialsLoading,
  } = useClickHouseCredentialStore();
  const { user, setCurrentCredential } = useAuthStore();
  const [open, setOpen] = React.useState(false);
  const [value, setValue] = React.useState("");

  // Fetch credentials on component mount
  React.useEffect(() => {
    fetchCredentials();
  }, [fetchCredentials]);

  // Update value when user's active credential changes
  React.useEffect(() => {
    if (user?.activeClickhouseCredential) {
      setValue(user.activeClickhouseCredential.toString());
    } else {
      setValue("");
    }
  }, [user?.activeClickhouseCredential]);

  // Handle selection
  const handleSelect = async (credentialId: string, credentialName: string) => {
    if (credentialId !== value) {
      setValue(credentialId);
      setOpen(false);
      try {
        await setCurrentCredential(credentialId);
        toast.success(`${credentialName} selected as current credential.`);
      } catch (error: any) {
        console.error("Update current credential failed:", error);
        toast.error(`Failed: ${error.message}`);
      }
    }
  };

  const selectedCredential =
    credentials.find((cred) => cred._id === value) ||
    user?.activeClickhouseCredential;

  if (isCredentialsLoading) {
    return <Button variant="outline">Loading credentials...</Button>;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="max-w-[180px] justify-between gap-2"
        >
          <Key className="h-4 w-4 shrink-0 opacity-50" />
          <span className="truncate">
            {selectedCredential ? selectedCredential.name : "Select credential"}
          </span>
          <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          <CommandInput placeholder="Search credential..." />
          <CommandEmpty>No credential found.</CommandEmpty>
          <CommandList>
            <CommandGroup>
              {credentials.map((cred) => (
                <CommandItem
                  key={cred._id}
                  value={cred._id}
                  onSelect={() => handleSelect(cred._id, cred.name)}
                >
                  <Check
                    className={cn(
                      "mr-2 h-4 w-4",
                      value === cred._id ? "opacity-100" : "opacity-0"
                    )}
                  />
                  {cred.name}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export default CredentialSelector;
