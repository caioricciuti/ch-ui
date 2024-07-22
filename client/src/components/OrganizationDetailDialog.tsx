// src/components/OrganizationDetailDialog.tsx
import React from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import useOrganizationStore from "@/stores/organization.store";

interface OrganizationDetailDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const OrganizationDetailDialog: React.FC<OrganizationDetailDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const { selectedOrganization } = useOrganizationStore();

  if (!selectedOrganization) return null;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{selectedOrganization.name}</DialogTitle>
        </DialogHeader>
        <div>
          <p>ID: {selectedOrganization._id}</p>
          <p>Slug: {selectedOrganization.slug}</p>
          <p>Number of Members: {selectedOrganization.members.length}</p>
          <p>
            Owner: {selectedOrganization.owner.name} (
            {selectedOrganization.owner.email})
          </p>
          <p>
            Created At:{" "}
            {new Date(selectedOrganization.createdAt).toLocaleString()}
          </p>
          <p>
            Updated At:{" "}
            {new Date(selectedOrganization.updatedAt).toLocaleString()}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default OrganizationDetailDialog;
