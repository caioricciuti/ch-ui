// components/UserTable/ResetPasswordDialog.tsx
import React from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

interface ResetPasswordDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  selectedUser: string | null;
  onResetPassword: (username: string) => void;
}

const ResetPasswordDialog: React.FC<ResetPasswordDialogProps> = ({
  open,
  onOpenChange,
  selectedUser,
  onResetPassword,
}) => {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reset Password</DialogTitle>
          <DialogDescription>
            Are you sure you want to reset the password for "{selectedUser}"?
            This action will generate a new password.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="outline"
            onClick={() => selectedUser && onResetPassword(selectedUser)}
          >
            Reset Password
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default ResetPasswordDialog;
