import * as React from "react";
import { Building2, ChevronsUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Alert, AlertDescription } from "@/components/ui/alert";
import useOrganizationStore from "@/stores/organization.store";
import useAuthStore from "@/stores/user.store";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import { toast } from "sonner";

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

  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [tempOrgValue, setTempOrgValue] = React.useState("");
  const [tempCredValue, setTempCredValue] = React.useState("");

  React.useEffect(() => {
    fetchOrganizations();
  }, [fetchOrganizations]);

  React.useEffect(() => {
    if (user?.activeOrganization?._id) {
      setTempOrgValue(user.activeOrganization._id);
      fetchAvailbleCredentialsAndHandleErrors(user.activeOrganization._id)
    }
    if (user?.activeClickhouseCredential?._id) {
      setTempCredValue(user.activeClickhouseCredential._id);
    }
  }, [
    user?.activeOrganization,
    user?.activeClickhouseCredential,
    fetchAvailableCredentials,
  ]);

  const handleDialogOpen = () => {
    setTempOrgValue(user?.activeOrganization?._id || "");
    setTempCredValue(user?.activeClickhouseCredential?._id || "");
    setDialogOpen(true);
  };

  const handleDialogClose = () => {
    setDialogOpen(false);
  };

  const fetchAvailbleCredentialsAndHandleErrors = async (organizationId: string) => {
    try {
      await fetchAvailableCredentials(organizationId);
    } catch(error) {
      if (error instanceof Error) {
        toast.error(error.message)
      } {
        toast.error("Failed to fetch available credentials")
      }
    }
  }

  const handleOrgSelect = async (organizationId: string) => {
    setTempOrgValue(organizationId);
    resetCredentials();
    setTempCredValue("");
    await fetchAvailbleCredentialsAndHandleErrors(organizationId)
  };

  const handleCredSelect = (credentialId: string) => {
    setTempCredValue(credentialId);
  };

  const handleSave = async () => {
    if (!tempOrgValue || !tempCredValue) {
      toast.error("Please select both an organization and a credential.");
      return;
    }

    try {
      await setCurrentOrganization(tempOrgValue);
      await setCurrentCredential(tempCredValue);
      toast.success("Organization and credential updated successfully.");
      setDialogOpen(false);
    } catch (error: any) {
      toast.error(`Failed to update: ${error.message}`);
    }
  };

  const noCredentialsAvailable =
    availableCredentials.length === 0 && tempOrgValue !== "" && !credLoading;

  return (
    <>
      <Button
        variant="outline"
        className={`max-w-[180px]  ${isExpanded ? "gap-2" : "max-w-[12px]"}`}
        onClick={handleDialogOpen}
      >
        <Building2 className="h-4 w-4 shrink-0 opacity-80" />

        {isExpanded && (
          <>
            <span className="truncate">
              {user?.activeOrganization?.name || "Select Organization"}
            </span>
            <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
          </>
        )}
      </Button>

      <Dialog open={dialogOpen} onOpenChange={handleDialogClose}>
        <DialogContent className="sm:max-w-[425px]">
          <DialogHeader>
            <DialogTitle>Choose Organization and Credential</DialogTitle>
            <DialogDescription>
              Select your organization and credential to continue.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="organization" className="text-right">
                Organization
              </Label>
              <Select
                value={tempOrgValue}
                onValueChange={handleOrgSelect}
                disabled={orgLoading}
              >
                <SelectTrigger className="col-span-3">
                  <SelectValue placeholder="Select organization" />
                </SelectTrigger>
                <SelectContent>
                  {organizations.map((org) => (
                    <SelectItem key={org._id} value={org._id}>
                      {org.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="credential" className="text-right">
                Credential
              </Label>
              <Select
                value={tempCredValue}
                onValueChange={handleCredSelect}
                disabled={
                  credLoading || !tempOrgValue || noCredentialsAvailable
                }
              >
                <SelectTrigger className="col-span-3">
                  <SelectValue placeholder="Select credential" />
                </SelectTrigger>
                <SelectContent>
                  {availableCredentials.map((cred) => (
                    <SelectItem key={cred._id} value={cred._id}>
                      {cred.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {noCredentialsAvailable && (
              <Alert variant="destructive">
                <AlertDescription>
                  There are no credentials available for this organization you
                  can use.
                </AlertDescription>
              </Alert>
            )}
          </div>
          <DialogFooter>
            <Button
              onClick={handleSave}
              disabled={
                !tempOrgValue || !tempCredValue || noCredentialsAvailable
              }
            >
              Save changes
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default CombinedSelector;
