// src/components/CredentialsDialog.tsx
import React, { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import useOrganizationStore from "@/stores/organization.store";
import api from "@/api/axios.config";

interface Credential {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
}

interface CredentialsDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const CredentialsDialog: React.FC<CredentialsDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const { selectedOrganization } = useOrganizationStore();
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen && selectedOrganization) {
      fetchCredentials();
    }
  }, [isOpen, selectedOrganization]);

  const fetchCredentials = async () => {
    if (!selectedOrganization) return;

    setIsLoading(true);
    setError(null);
    try {
      const response = await api.get(
        `/organizations/${selectedOrganization._id}/credentials`
      );
      setCredentials(response.data);
    } catch (err) {
      setError("Failed to fetch credentials");
      console.error(err);
    } finally {
      setIsLoading(false);
    }
  };

  const handleAssignCredential = async (credentialId: string) => {
    if (!selectedOrganization) return;

    try {
      await api.post(`/clickhouse-credentials/${credentialId}/organizations`, {
        organizationId: selectedOrganization._id,
      });
      await fetchCredentials();
    } catch (err) {
      setError("Failed to assign credential");
      console.error(err);
    }
  };

  const handleRevokeCredential = async (credentialId: string) => {
    if (!selectedOrganization) return;

    try {
      await api.delete(
        `/clickhouse-credentials/${credentialId}/organizations/${selectedOrganization._id}`
      );
      await fetchCredentials();
    } catch (err) {
      setError("Failed to revoke credential");
      console.error(err);
    }
  };

  if (!selectedOrganization) return null;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            Manage Credentials for {selectedOrganization.name}
          </DialogTitle>
        </DialogHeader>
        {isLoading ? (
          <p>Loading credentials...</p>
        ) : error ? (
          <p className="text-red-500">{error}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Host</TableHead>
                <TableHead>Port</TableHead>
                <TableHead>Username</TableHead>
                <TableHead>Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {credentials.map((credential) => (
                <TableRow key={credential.id}>
                  <TableCell>{credential.name}</TableCell>
                  <TableCell>{credential.host}</TableCell>
                  <TableCell>{credential.port}</TableCell>
                  <TableCell>{credential.username}</TableCell>
                  <TableCell>
                    <Button
                      variant="outline"
                      onClick={() => handleRevokeCredential(credential.id)}
                    >
                      Revoke
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="mt-4">
          <h3 className="text-lg font-semibold mb-2">Assign New Credential</h3>
          {/* Here you would typically have a dropdown or search to select a credential to assign */}
          {/* For simplicity, let's just have a button that would open another dialog to select a credential */}
          <Button
            onClick={() => {
              /* Open credential selection dialog */
            }}
          >
            Assign Credential
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default CredentialsDialog;
