import { useState, useEffect, useCallback } from "react";
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
    isLoading: isOrgLoading,
    error: orgError,
  } = useOrganizationStore();
  const {
    availableCredentials,
    fetchAvailableCredentials,
    isLoading: isCredLoading,
    error: credError,
  } = useClickHouseCredentialStore();
  const {
    setCurrentOrganization,
    setCurrentCredential,
    getActiveOrganization,
    getActiveCredential,
  } = useAuthStore();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [tempOrgValue, setTempOrgValue] = useState("");
  const [tempCredValue, setTempCredValue] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const fetchOrganizationsAndHandleErrors = useCallback(async () => {
    try {
      await fetchOrganizations();
    } catch (error) {
      console.error("Failed to fetch organizations:", error);
    }
  }, [fetchOrganizations]);

  const fetchAvailableCredentialsAndHandleErrors = useCallback(
    async (organizationId: string) => {
      try {
        await fetchAvailableCredentials(organizationId);
      } catch (error) {
        console.error("Failed to fetch available credentials:", error);
      }
    },
    [fetchAvailableCredentials]
  );

  useEffect(() => {
    if (dialogOpen) {
      fetchOrganizationsAndHandleErrors();
      const activeOrg = getActiveOrganization();
      const activeCred = getActiveCredential();

      if (activeOrg) {
        setTempOrgValue(activeOrg._id);
        fetchAvailableCredentialsAndHandleErrors(activeOrg._id);
      }
      if (activeCred) {
        setTempCredValue(activeCred._id);
      }
    }
  }, [
    dialogOpen,
    getActiveOrganization,
    getActiveCredential,
    fetchOrganizationsAndHandleErrors,
    fetchAvailableCredentialsAndHandleErrors,
  ]);

  useEffect(() => {
    if (orgError && !isOrgLoading) {
      toast.error(`Organization error: ${orgError}`);
    }
  }, [orgError, isOrgLoading]);

  useEffect(() => {
    if (credError && !isCredLoading) {
      toast.error(`Credential error: ${credError}`);
    }
  }, [credError, isCredLoading]);

  const handleDialogOpen = () => {
    setDialogOpen(true);
  };

  const handleDialogClose = () => {
    setDialogOpen(false);
  };

  const handleOrgSelect = async (organizationId: string) => {
    setTempOrgValue(organizationId);
    setTempCredValue("");
    await fetchAvailableCredentialsAndHandleErrors(organizationId);
  };

  const handleCredSelect = (credentialId: string) => {
    setTempCredValue(credentialId);
  };

  const handleSave = async () => {
    if (!tempOrgValue || !tempCredValue) {
      toast.error("Please select both an organization and a credential.");
      return;
    }

    setIsSaving(true);
    try {
      await setCurrentOrganization(tempOrgValue);
      await setCurrentCredential(tempCredValue);
      toast.success("Organization and credential updated successfully.");
      setDialogOpen(false);
    } catch (error) {
      if (error instanceof Error) {
        toast.error(`Failed to update: ${error.message}`);
      } else {
        toast.error("An unexpected error occurred while updating");
      }
    } finally {
      setIsSaving(false);
    }
  };

  const noCredentialsAvailable =
    availableCredentials.length === 0 && tempOrgValue !== "" && !isCredLoading;

  const activeOrg = getActiveOrganization();

  // Determine if there are no organizations or no credentials
  const noOrganizations = organizations.length === 0;
  const noCredentialsForSelectedOrg = noCredentialsAvailable;
  const showGeneralAlert = noOrganizations || noCredentialsForSelectedOrg;

  return (
    <>
      <Button
        variant="outline"
        className={`max-w-[180px] ${isExpanded ? "gap-2" : "max-w-[12px]"}`}
        onClick={handleDialogOpen}
      >
        <Building2 className="h-4 w-4 shrink-0 opacity-80" />
        {isExpanded && (
          <>
            <span className="truncate">
              {activeOrg?.name || "Select Organization"}
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
                disabled={isOrgLoading || isSaving || noOrganizations}
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
                  isCredLoading ||
                  !tempOrgValue ||
                  noCredentialsAvailable ||
                  isSaving
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

            {/* Existing Alert for No Credentials Available */}
            {noCredentialsAvailable && (
              <Alert variant="destructive">
                <AlertDescription>
                  There are no credentials available for this organization you
                  can use.
                </AlertDescription>
              </Alert>
            )}

            {/* New General Alert for No Organizations or Credentials */}
            {showGeneralAlert && (
              <Alert variant="destructive">
                <AlertDescription>
                  You need to have at least 1 Organization and 1 Credential to
                  choose from. You can create your own or ask your admin to
                  invite you.
                </AlertDescription>
              </Alert>
            )}
          </div>
          <DialogFooter>
            <Button onClick={handleDialogClose} variant="outline">
              Cancel
            </Button>
            <Button
              onClick={handleSave}
              disabled={
                !tempOrgValue ||
                !tempCredValue ||
                noCredentialsAvailable ||
                isOrgLoading ||
                isCredLoading ||
                isSaving ||
                noOrganizations
              }
            >
              {isSaving ? "Saving..." : "Save changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default CombinedSelector;
