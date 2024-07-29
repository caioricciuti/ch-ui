// CredentialList component

import React, { useState, useEffect, useMemo } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  MoreHorizontal,
  Eye,
  Edit,
  Trash,
  Users,
  UserPlus,
  UserMinus,
  PlusCircle,
  MinusCircle,
  Building2,
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import useAuthStore from "@/stores/user.store";
import useOrganizationStore from "@/stores/organization.store";
import { ClickHouseCredential } from "@/types/types";
import { ScrollArea } from "@/components/ui/scroll-area";

interface CredentialListProps {
  credentials: ClickHouseCredential[];
  onViewDetails: (cred: ClickHouseCredential) => void;
  onEdit: (cred: ClickHouseCredential) => void;
  onDelete: (cred: ClickHouseCredential) => void;
}

const CredentialList: React.FC<CredentialListProps> = ({
  credentials,
  onViewDetails,
  onEdit,
  onDelete,
}) => {
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [addUserDialogOpen, setAddUserDialogOpen] = useState(false);
  const [removeUserDialogOpen, setRemoveUserDialogOpen] = useState(false);
  const [addOrgDialogOpen, setAddOrgDialogOpen] = useState(false);
  const [removeOrgDialogOpen, setRemoveOrgDialogOpen] = useState(false);
  const [credentialToManage, setCredentialToManage] =
    useState<ClickHouseCredential | null>(null);
  const [selectedUserId, setSelectedUserId] = useState<string>("");
  const [selectedOrgId, setSelectedOrgId] = useState<string>("");

  const { getAllUsers, allUsers } = useAuthStore();
  const { organizations, fetchOrganizations } = useOrganizationStore();
  const {
    assignUserToCredential,
    revokeUserFromCredential,
    assignCredentialToOrganization,
    revokeCredentialFromOrganization,
    fetchAvailableCredentials,
    fetchCredentials,
  } = useClickHouseCredentialStore();

  useEffect(() => {
    getAllUsers();
    fetchOrganizations();
    setCredentialToManage(credentials[0]);
  }, [getAllUsers, fetchOrganizations]);

  const credentialUserIds = useMemo(() => {
    if (!credentialToManage) return new Set<any>();
    return new Set(credentialToManage.users.map((user: any) => user._id));
  }, [credentialToManage]);

  const credentialOrgIds = useMemo(() => {
    if (!credentialToManage) return new Set<any>();
    return new Set(
      credentialToManage.allowedOrganizations.map((org: any) => org._id)
    );
  }, [credentialToManage]);

  const usersNotInCredential = useMemo(() => {
    if (!credentialToManage) return [];
    return allUsers.filter((user) => !credentialUserIds.has(user._id));
  }, [allUsers, credentialToManage]);

  const usersInCredential = useMemo(() => {
    if (!credentialToManage) return [];
    return allUsers.filter((user) =>
      credentialToManage.users.map((u: any) => u._id).includes(user._id)
    );
  }, [allUsers, credentialToManage]);

  const orgsNotInCredential = useMemo(() => {
    if (!credentialToManage) return [];
    return organizations.filter((org) => !credentialOrgIds.has(org._id));
  }, [organizations, credentialToManage]);

  const orgsInCredential = useMemo(() => {
    if (!credentialToManage) return [];
    return organizations.filter((org) =>
      credentialToManage.allowedOrganizations
        .map((o: any) => o._id)
        .includes(org._id)
    );
  }, [organizations, credentialToManage]);

  const handleDeleteClick = (cred: ClickHouseCredential) => {
    setCredentialToManage(cred);
    setDeleteDialogOpen(true);
  };

  const handleConfirmDelete = () => {
    if (credentialToManage) {
      onDelete(credentialToManage);
      setDeleteDialogOpen(false);
      setCredentialToManage(null);
      fetchAvailableCredentials();
      fetchCredentials();
    }
  };

  const handleAddUserClick = (cred: ClickHouseCredential) => {
    setCredentialToManage(cred);
    setAddUserDialogOpen(true);
  };

  const handleRemoveUserClick = (cred: ClickHouseCredential) => {
    setCredentialToManage(cred);
    setRemoveUserDialogOpen(true);
  };

  const handleAddOrgClick = (cred: ClickHouseCredential) => {
    setCredentialToManage(cred);
    setAddOrgDialogOpen(true);
  };

  const handleRemoveOrgClick = (cred: ClickHouseCredential) => {
    setCredentialToManage(cred);
    setRemoveOrgDialogOpen(true);
  };

  const handleConfirmAddUser = async () => {
    if (credentialToManage && selectedUserId) {
      await assignUserToCredential(credentialToManage._id, selectedUserId);
      setAddUserDialogOpen(false);
      setCredentialToManage(null);
      setSelectedUserId("");
    }
  };

  const handleConfirmRemoveUser = async () => {
    if (credentialToManage && selectedUserId) {
      await revokeUserFromCredential(credentialToManage._id, selectedUserId);
      setRemoveUserDialogOpen(false);
      setCredentialToManage(null);
      setSelectedUserId("");
    }
  };

  const handleConfirmAddOrg = async () => {
    if (credentialToManage && selectedOrgId) {
      await assignCredentialToOrganization(
        credentialToManage._id,
        selectedOrgId
      );
      setAddOrgDialogOpen(false);
      setCredentialToManage(null);
      setSelectedOrgId("");
      fetchCredentials();
      fetchOrganizations();
      fetchAvailableCredentials();
    }
  };

  const handleConfirmRemoveOrg = async () => {
    if (credentialToManage && selectedOrgId) {
      await revokeCredentialFromOrganization(
        credentialToManage._id,
        selectedOrgId
      );
      setRemoveOrgDialogOpen(false);
      setCredentialToManage(null);
      setSelectedOrgId("");
    }
  };

  return (
    <div className="h-[30vh]">
      <ScrollArea className="h-[calc(100vh-20rem)] w-full">
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Host</TableHead>
                <TableHead>Port</TableHead>
                <TableHead>Users</TableHead>
                <TableHead>Organizations</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {credentials.map((cred) => (
                <TableRow key={cred._id}>
                  <TableCell className="font-medium">{cred.name}</TableCell>
                  <TableCell>{cred.host}</TableCell>
                  <TableCell>{cred.port}</TableCell>
                  <TableCell>
                    <div className="flex items-center">
                      <Users className="mr-2 h-4 w-4" />
                      {cred.users.length}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center">
                      <Building2 className="mr-2 h-4 w-4" />
                      {cred.allowedOrganizations.length}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" className="h-8 w-8 p-0">
                          <span className="sr-only">Open menu</span>
                          <MoreHorizontal className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuLabel>Actions</DropdownMenuLabel>
                        <DropdownMenuItem onClick={() => onViewDetails(cred)}>
                          <Eye className="mr-2 h-4 w-4" />
                          View details
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => onEdit(cred)}>
                          <Edit className="mr-2 h-4 w-4" />
                          Edit
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => handleAddUserClick(cred)}
                        >
                          <UserPlus className="mr-2 h-4 w-4" />
                          Add User
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => handleRemoveUserClick(cred)}
                        >
                          <UserMinus className="mr-2 h-4 w-4" />
                          Remove User
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => handleAddOrgClick(cred)}
                        >
                          <PlusCircle className="mr-2 h-4 w-4" />
                          Add Organization
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => handleRemoveOrgClick(cred)}
                        >
                          <MinusCircle className="mr-2 h-4 w-4" />
                          Remove Organization
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          onClick={() => handleDeleteClick(cred)}
                          className="text-red-600"
                        >
                          <Trash className="mr-2 h-4 w-4" />
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Confirm Deletion</DialogTitle>
                <DialogDescription>
                  Are you sure you want to delete the credential "
                  {credentialToManage?.name}"? This action cannot be undone.
                  <Alert variant="warning" className="mt-4">
                    When you delete a credential, all associated users and
                    organizations will lose access to this ClickHouse instance.
                  </Alert>
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setDeleteDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button variant="destructive" onClick={handleConfirmDelete}>
                  Delete
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          <Dialog open={addUserDialogOpen} onOpenChange={setAddUserDialogOpen}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Add User to Credential</DialogTitle>
                <DialogDescription>
                  Select a user to add to "{credentialToManage?.name}".
                </DialogDescription>
              </DialogHeader>
              <Select onValueChange={setSelectedUserId} value={selectedUserId}>
                <SelectTrigger disabled={usersNotInCredential.length === 0}>
                  <SelectValue
                    placeholder={
                      usersNotInCredential.length === 0
                        ? "No users available to add"
                        : "Select a user"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {usersNotInCredential.map((user) => (
                    <SelectItem key={user._id} value={user._id}>
                      {user.name} ({user.email})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {usersNotInCredential.length === 0 && (
                <DialogDescription className="text-red-600 text-xs">
                  There are no users available to add to this credential. All
                  users are already assigned or no users exist in the system.
                </DialogDescription>
              )}
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setAddUserDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleConfirmAddUser}
                  disabled={!selectedUserId}
                >
                  Add User
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          <Dialog
            open={removeUserDialogOpen}
            onOpenChange={setRemoveUserDialogOpen}
          >
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Remove User from Credential</DialogTitle>
                <DialogDescription>
                  Select a user to remove from "{credentialToManage?.name}".
                </DialogDescription>
              </DialogHeader>
              <Select onValueChange={setSelectedUserId} value={selectedUserId}>
                <SelectTrigger disabled={usersInCredential.length === 0}>
                  <SelectValue
                    placeholder={
                      usersInCredential.length === 0
                        ? "No users available to remove"
                        : "Select a user"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {usersInCredential.map((user) => (
                    <SelectItem key={user._id} value={user._id}>
                      {user.name} ({user.email})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {usersInCredential.length === 0 && (
                <DialogDescription className="text-red-600 text-xs">
                  There are no users to remove from this credential. The
                  credential has no assigned users.
                </DialogDescription>
              )}
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setRemoveUserDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleConfirmRemoveUser}
                  disabled={!selectedUserId}
                  variant="destructive"
                >
                  Remove User
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          <Dialog open={addOrgDialogOpen} onOpenChange={setAddOrgDialogOpen}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Add Organization to Credential</DialogTitle>
                <DialogDescription>
                  Select an organization to add to "{credentialToManage?.name}".
                </DialogDescription>
              </DialogHeader>
              <Select onValueChange={setSelectedOrgId} value={selectedOrgId}>
                <SelectTrigger disabled={orgsNotInCredential.length === 0}>
                  <SelectValue
                    placeholder={
                      orgsNotInCredential.length === 0
                        ? "No organizations available to add"
                        : "Select an organization"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {orgsNotInCredential.map((org) => (
                    <SelectItem key={org._id} value={org._id}>
                      {org.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {orgsNotInCredential.length === 0 && (
                <DialogDescription className="text-red-600 text-xs">
                  There are no organizations available to add to this
                  credential. All organizations are already assigned or no
                  organizations exist in the system.
                </DialogDescription>
              )}
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setAddOrgDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button onClick={handleConfirmAddOrg} disabled={!selectedOrgId}>
                  Add Organization
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          <Dialog
            open={removeOrgDialogOpen}
            onOpenChange={setRemoveOrgDialogOpen}
          >
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Remove Organization from Credential</DialogTitle>
                <DialogDescription>
                  Select an organization to remove from "
                  {credentialToManage?.name}
                  ".
                </DialogDescription>
              </DialogHeader>
              <Select onValueChange={setSelectedOrgId} value={selectedOrgId}>
                <SelectTrigger disabled={orgsInCredential.length === 0}>
                  <SelectValue
                    placeholder={
                      orgsInCredential.length === 0
                        ? "No organizations available to remove"
                        : "Select an organization"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {orgsInCredential.map((org) => (
                    <SelectItem key={org._id} value={org._id}>
                      {org.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {orgsInCredential.length === 0 && (
                <DialogDescription className="text-red-600 text-xs">
                  There are no organizations to remove from this credential. The
                  credential has no assigned organizations.
                </DialogDescription>
              )}
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setRemoveOrgDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleConfirmRemoveOrg}
                  disabled={!selectedOrgId}
                  variant="destructive"
                >
                  Remove Organization
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </>
      </ScrollArea>
    </div>
  );
};

export default CredentialList;
