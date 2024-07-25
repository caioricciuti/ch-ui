import * as React from "react";
import { Building2, Check, ChevronsUpDown } from "lucide-react";
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
import { toast } from "sonner";

export function OrganizationCombobox() {
  const { organizations, fetchOrganizations } = useOrganizationStore();
  const { user, setCurrentOrganization } = useAuthStore();
  const [open, setOpen] = React.useState(false);
  const [value, setValue] = React.useState("");

  // Fetch organizations on component mount
  React.useEffect(() => {
    fetchOrganizations();
  }, [fetchOrganizations]);

  // Update value when user's active organization changes
  React.useEffect(() => {
    if (user?.activeOrganization) {
      setValue(user.activeOrganization._id);
    } else {
      setValue("");
    }
  }, [user?.activeOrganization]);

  // Handle selection
  const handleSelect = async (
    organizationId: string,
    organizationName: string
  ) => {
    if (organizationId !== value) {
      setValue(organizationId);
      setOpen(false);
      try {
        await setCurrentOrganization(organizationId);
        toast.success(`${organizationName} selected as current organization.`);
      } catch (error: any) {
        console.error("Update current organizaion failed:", error);
        toast.error(`Failed: ${error.message}`);
      }
    }
  };

  const selectedOrganization = organizations.find((org) => org._id === value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="max-w-[180px] justify-between gap-2"
        >
          <Building2 className="h-4 w-4" />
          <span className="truncate">
            {selectedOrganization
              ? selectedOrganization.name
              : "Select organization"}
          </span>
          <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          <CommandInput placeholder="Search organization..." />
          <CommandEmpty>No organization found.</CommandEmpty>
          <CommandList>
            <CommandGroup>
              {organizations.map((org) => (
                <CommandItem
                  key={org._id}
                  value={org._id}
                  onSelect={() => handleSelect(org._id, org.name)}
                >
                  <Check
                    className={cn(
                      "mr-2 h-4 w-4",
                      value === org._id ? "opacity-100" : "opacity-0"
                    )}
                  />
                  {org.name}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export default OrganizationCombobox;
