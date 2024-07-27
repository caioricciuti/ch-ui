import * as React from "react";
import { Building2, Check, ChevronsUpDown, Loader2, Key } from "lucide-react";
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
import useOrganizationStore from "@/stores/organization.store";
import useAuthStore from "@/stores/user.store";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import { toast } from "sonner";

export function CombinedSelector() {
  const {
    organizations,
    fetchOrganizations,
    isLoading: orgLoading,
  } = useOrganizationStore();
  const {
    availableCredentials,
    isLoading: credLoading,
    fetchAvailableCredentials,
    resetCredentials,
  } = useClickHouseCredentialStore();
  const { user, setCurrentOrganization, setCurrentCredential } = useAuthStore();

  const [orgOpen, setOrgOpen] = React.useState(false);
  const [credOpen, setCredOpen] = React.useState(false);
  const [orgValue, setOrgValue] = React.useState("");
  const [credValue, setCredValue] = React.useState("");

  React.useEffect(() => {
    fetchOrganizations();
    fetchAvailableCredentials();
  }, [fetchOrganizations, fetchAvailableCredentials]);

  React.useEffect(() => {
    if (user?.activeOrganization) {
      setOrgValue(user.activeOrganization._id);
    }
    if (user?.activeClickhouseCredential) {
      setCredValue(user.activeClickhouseCredential._id);
    }
  }, [user?.activeOrganization, user?.activeClickhouseCredential]);

  const handleOrgSelect = async (
    organizationId: string,
    organizationName: string
  ) => {
    if (organizationId !== orgValue) {
      setOrgValue(organizationId);
      setOrgOpen(false);
      try {
        await setCurrentOrganization(organizationId);
        resetCredentials();
        fetchAvailableCredentials();
        toast.success(`${organizationName} selected as current organization. Please select a credential.`);
        // force user to select a credential after selecting an organization
        setCredOpen(true);  
      } catch (error: any) {
        console.error("Update current organization failed:", error);
        toast.error(`Failed: ${error.message}`);
      }
    }
  };

  const handleCredSelect = async (
    credentialId: string,
    credentialName: string
  ) => {
    if (credentialId !== credValue) {
      setCredValue(credentialId);
      setCredOpen(false);
      try {
        await setCurrentCredential(credentialId);
        toast.success(`${credentialName} selected as current credential.`);
      } catch (error: any) {
        console.error("Update current credential failed:", error);
        toast.error(`Failed: ${error.message}`);
      }
    }
  };

  const selectedOrganization = organizations.find(
    (org) => org._id === orgValue
  );
  const selectedCredential = availableCredentials.find(
    (cred) => cred._id === credValue
  );

  const renderSelector = (
    type: "org" | "cred",
    loading: boolean,
    open: boolean,
    setOpen: React.Dispatch<React.SetStateAction<boolean>>,
    selected: any,
    items: any[],
    handleSelect: (id: string, name: string) => void
  ) => {
    const icon = type === "org" ? Building2 : Key;
    const placeholder =
      type === "org" ? "Select organization" : "Select credential";
    const searchPlaceholder =
      type === "org" ? "Search organization..." : "Search credential...";
    const emptyMessage =
      type === "org" ? "No organization found." : "No credential found.";

    if (loading) {
      return (
        <Button
          variant="outline"
          className="max-w-[180px] justify-between gap-2"
        >
          {React.createElement(icon, {
            className: "h-4 w-4 shrink-0 opacity-50 mr-2",
          })}
          <Loader2 className="h-4 w-4 animate-spin" />
        </Button>
      );
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
            {React.createElement(icon, {
              className: "h-4 w-4 shrink-0 opacity-50",
            })}
            <span className="truncate">
              {selected ? selected.name : placeholder}
            </span>
            <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[200px] p-0">
          <Command>
            <CommandInput placeholder={searchPlaceholder} />
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandList>
              <CommandGroup>
                {items.map((item) => (
                  <CommandItem
                    key={item._id}
                    value={item._id}
                    onSelect={() => handleSelect(item._id, item.name)}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        selected?._id === item._id ? "opacity-100" : "opacity-0"
                      )}
                    />
                    {item.name}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    );
  };

  return (
    <div className="flex space-x-2">
      {renderSelector(
        "org",
        orgLoading,
        orgOpen,
        setOrgOpen,
        selectedOrganization,
        organizations,
        handleOrgSelect
      )}
      {renderSelector(
        "cred",
        credLoading,
        credOpen,
        setCredOpen,
        selectedCredential,
        availableCredentials,
        handleCredSelect
      )}
    </div>
  );
}

export default CombinedSelector;
