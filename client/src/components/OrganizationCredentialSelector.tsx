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
import ConfirmationDialog from "@/components/ConfirmationDialog";

export function CombinedSelector({ isExpanded }: { isExpanded: boolean }) {
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
  const [confirmDialogOpen, setConfirmDialogOpen] = React.useState(false);
  const [pendingOrgChange, setPendingOrgChange] = React.useState<{
    id: string;
    name: string;
  } | null>(null);

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
      setPendingOrgChange({ id: organizationId, name: organizationName });
      setConfirmDialogOpen(true);
      setOrgOpen(false);
    }
  };

  const confirmOrgChange = async () => {
    if (pendingOrgChange) {
      setOrgValue(pendingOrgChange.id);
      try {
        await setCurrentOrganization(pendingOrgChange.id);
        resetCredentials();
        fetchAvailableCredentials();
        toast.success(
          `${pendingOrgChange.name} selected as current organization. Please select a credential.`
        );
        setCredOpen(true);
      } catch (error: any) {
        console.error("Update current organization failed:", error);
        toast.error(`Failed: ${error.message}`);
      }
    }
    setConfirmDialogOpen(false);
    setPendingOrgChange(null);
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
        window.dispatchEvent(new Event("workspaceReload"));
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
            className={`max-w-[180px] justify-between gap-2
              ${isExpanded ? "" : "hidden"}
              `}
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
    <div className="w-full space-y-2">
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
      <ConfirmationDialog
        isOpen={confirmDialogOpen}
        onClose={() => setConfirmDialogOpen(false)}
        onConfirm={confirmOrgChange}
        title="Confirm Organization Change"
        description={`Are you sure you want to change the organization to ${pendingOrgChange?.name}? This will reset your current credential selection.`}
        confirmText="Change Organization"
        cancelText="Cancel"
      />
    </div>
  );
}

export default CombinedSelector;
