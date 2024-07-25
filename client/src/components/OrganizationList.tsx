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
} from "lucide-react";
import useAuthStore from "@/stores/user.store";
import useOrganizationStore from "@/stores/organization.store";
import { Alert } from "./ui/alert";

interface Member {
  _id: string;
  name: string;
  email: string;
}

interface Organization {
  _id: string;
  name: string;
  slug: string;
  members: Member[];
  owner: Member;
  createdAt: string;
  updatedAt: string;
}

interface OrganizationListProps {
  organizations: Organization[];
  onViewDetails: (org: Organization) => void;
  onEdit: (org: Organization) => void;
  onDelete: (org: Organization) => void;
}

const OrganizationList: React.FC<OrganizationListProps> = ({
  organizations,
  onViewDetails,
  onEdit,
  onDelete,
}) => {
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [addUserDialogOpen, setAddUserDialogOpen] = useState(false);
  const [removeUserDialogOpen, setRemoveUserDialogOpen] = useState(false);
  const [organizationToDelete, setOrganizationToDelete] =
    useState<Organization | null>(null);
  const [organizationToManage, setOrganizationToManage] =
    useState<Organization | null>(null);
  const [selectedUserId, setSelectedUserId] = useState<string>("");

  const { getAllUsers, allUsers, user: currentUser } = useAuthStore();
  const { addUserToOrganization, removeUserFromOrganization } =
    useOrganizationStore();

  // Filter users not in the organization
  const usersNotInOrganization = useMemo(() => {
    if (!organizationToManage) return [];
    const memberIds = new Set(
      organizationToManage.members.map((member) => member._id)
    );
    return allUsers.filter((user) => !memberIds.has(user._id));
  }, [allUsers, organizationToManage]);

  const removableUsers = useMemo(() => {
    if (!organizationToManage) return [];
    return organizationToManage.members.filter(
      (member) => member._id !== organizationToManage.owner._id
    );
  }, [organizationToManage]);

  useEffect(() => {
    getAllUsers();
  }, [getAllUsers]);

  const handleDeleteClick = (org: Organization) => {
    setOrganizationToDelete(org);
    setDeleteDialogOpen(true);
  };

  const handleConfirmDelete = () => {
    if (organizationToDelete) {
      onDelete(organizationToDelete);
      setDeleteDialogOpen(false);
      setOrganizationToDelete(null);
    }
  };

  const handleAddUserClick = (org: Organization) => {
    setOrganizationToManage(org);
    setAddUserDialogOpen(true);
  };

  const handleRemoveUserClick = (org: Organization) => {
    setOrganizationToManage(org);
    setRemoveUserDialogOpen(true);
  };

  const handleConfirmAddUser = async () => {
    if (organizationToManage && selectedUserId) {
      await addUserToOrganization(organizationToManage._id, selectedUserId);
      setAddUserDialogOpen(false);
      setOrganizationToManage(null);
      setSelectedUserId("");
    }
  };

  const handleConfirmRemoveUser = async () => {
    if (organizationToManage && selectedUserId) {
      await removeUserFromOrganization(
        organizationToManage._id,
        selectedUserId
      );
      setRemoveUserDialogOpen(false);
      setOrganizationToManage(null);
      setSelectedUserId("");
    }
  };

  const isOwner = (org: Organization) => org.owner._id === currentUser?._id;

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Slug</TableHead>
            <TableHead>Members</TableHead>
            <TableHead>Owner</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {organizations.map((org) => (
            <TableRow key={org._id}>
              <TableCell className="font-medium">{org.name}</TableCell>
              <TableCell>{org.slug}</TableCell>
              <TableCell>
                <div className="flex items-center">
                  <Users className="mr-2 h-4 w-4" />
                  {org.members.length}
                </div>
              </TableCell>
              <TableCell>{org.owner.name}</TableCell>
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
                    <DropdownMenuItem onClick={() => onViewDetails(org)}>
                      <Eye className="mr-2 h-4 w-4" />
                      View details
                    </DropdownMenuItem>
                    {isOwner(org) && (
                      <>
                        <DropdownMenuItem onClick={() => onEdit(org)}>
                          <Edit className="mr-2 h-4 w-4" />
                          Edit
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => handleAddUserClick(org)}
                        >
                          <UserPlus className="mr-2 h-4 w-4" />
                          Add User
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => handleRemoveUserClick(org)}
                        >
                          <UserMinus className="mr-2 h-4 w-4" />
                          Remove User
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          onClick={() => handleDeleteClick(org)}
                          className="text-red-600"
                        >
                          <Trash className="mr-2 h-4 w-4" />
                          Delete
                        </DropdownMenuItem>
                      </>
                    )}
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
              Are you sure you want to delete the organization "
              {organizationToDelete?.name}"? This action cannot be undone.
              <Alert variant="warning" className="mt-4">
                When you delete an organization, all its members will lose
                access to the organization and its resources.
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
            <DialogTitle>Add User to Organization</DialogTitle>
            <DialogDescription>
              Select a user to add to "{organizationToManage?.name}".
            </DialogDescription>
          </DialogHeader>
          <Select onValueChange={setSelectedUserId} value={selectedUserId}>
            <SelectTrigger disabled={usersNotInOrganization.length === 0}>
              <SelectValue
                placeholder={
                  usersNotInOrganization.length === 0
                    ? "No users available to add"
                    : "Select a user"
                }
              />
            </SelectTrigger>
            <SelectContent>
              {usersNotInOrganization.map((user) => (
                <SelectItem key={user._id} value={user._id}>
                  {user.name} ({user.email})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {usersNotInOrganization.length === 0 && (
            <DialogDescription className="text-red-600 text-xs">
              There are no users available to add to this organization. Please
              invite users to the platform first.
            </DialogDescription>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setAddUserDialogOpen(false)}
            >
              Cancel
            </Button>
            <Button onClick={handleConfirmAddUser} disabled={!selectedUserId}>
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
            <DialogTitle>Remove User from Organization</DialogTitle>
            <DialogDescription>
              Select a user to remove from "{organizationToManage?.name}".
            </DialogDescription>
          </DialogHeader>
          <Select onValueChange={setSelectedUserId} value={selectedUserId}>
            <SelectTrigger disabled={removableUsers.length === 0}>
              <SelectValue
                placeholder={
                  removableUsers.length === 0
                    ? "No users available to remove"
                    : "Select a user"
                }
              />
            </SelectTrigger>
            <SelectContent>
              {removableUsers.map((member) => (
                <SelectItem key={member._id} value={member._id}>
                  {member.name} ({member.email})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {removableUsers.length === 0 && (
            <DialogDescription className="text-red-600 text-xs">
              There are no users to remove from this organization, the owner is
              the only member. Please delete the organization instead.
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
    </>
  );
};

export default OrganizationList;
