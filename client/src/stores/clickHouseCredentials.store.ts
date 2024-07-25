import { create } from "zustand";
import api from "@/api/axios.config";
import { toast } from "sonner";

interface ClickHouseCredential {
  _id: string;
  name: string;
  slug: string;
  host: string;
  port: number;
  username: string;
  users: string[];
  allowedOrganizations: string[];
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

interface ClickHouseCredentialState {
  credentials: ClickHouseCredential[];
  selectedCredential: ClickHouseCredential | null;
  isLoading: boolean;
  error: string | null;
  fetchCredentials: () => Promise<void>;
  createCredential: (
    credentialData: Partial<ClickHouseCredential>
  ) => Promise<void>;
  updateCredential: (
    id: string,
    credentialData: Partial<ClickHouseCredential>
  ) => Promise<void>;
  deleteCredential: (id: string) => Promise<void>;
  assignCredentialToOrganization: (
    credentialId: string,
    organizationId: string
  ) => Promise<void>;
  revokeCredentialFromOrganization: (
    credentialId: string,
    organizationId: string
  ) => Promise<void>;
  assignUserToCredential: (
    credentialId: string,
    userId: string
  ) => Promise<void>;
  revokeUserFromCredential: (
    credentialId: string,
    userId: string
  ) => Promise<void>;
  setSelectedCredential: (credential: ClickHouseCredential | null) => void;
}

const useClickHouseCredentialStore = create<ClickHouseCredentialState>(
  (set, get) => ({
    credentials: [],
    selectedCredential: null,
    isLoading: false,
    error: null,

    fetchCredentials: async () => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.get("/clickhouse-credentials");
        set({ credentials: response.data, isLoading: false });
      } catch (error) {
        set({ error: "Failed to fetch credentials", isLoading: false });
        toast.error("Failed to fetch credentials");
      }
    },

    createCredential: async (credentialData) => {
      set({ isLoading: true, error: null });
      try {
        await api.post("/clickhouse-credentials", credentialData);
        await get().fetchCredentials();
        toast.success("Credential created successfully");
      } catch (error) {
        set({ error: "Failed to create credential", isLoading: false });
        toast.error("Failed to create credential");
      }
    },

    updateCredential: async (id, credentialData) => {
      set({ isLoading: true, error: null });
      try {
        await api.put(`/clickhouse-credentials/${id}`, credentialData);
        await get().fetchCredentials();
        toast.success("Credential updated successfully");
      } catch (error) {
        set({ error: "Failed to update credential", isLoading: false });
        toast.error("Failed to update credential");
      }
    },

    deleteCredential: async (id) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(`/clickhouse-credentials/${id}`);
        await get().fetchCredentials();
        toast.success("Credential deleted successfully");
      } catch (error) {
        set({ error: "Failed to delete credential", isLoading: false });
        toast.error("Failed to delete credential");
      }
    },

    assignCredentialToOrganization: async (credentialId, organizationId) => {
      set({ isLoading: true, error: null });
      try {
        await api.post(
          `/clickhouse-credentials/${credentialId}/organizations`,
          { organizationId }
        );
        await get().fetchCredentials();
        toast.success("Credential assigned to organization successfully");
      } catch (error) {
        set({
          error: "Failed to assign credential to organization",
          isLoading: false,
        });
        toast.error("Failed to assign credential to organization");
      }
    },

    revokeCredentialFromOrganization: async (credentialId, organizationId) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/organizations/${organizationId}`
        );
        await get().fetchCredentials();
        toast.success("Credential revoked from organization successfully");
      } catch (error) {
        set({
          error: "Failed to revoke credential from organization",
          isLoading: false,
        });
        toast.error("Failed to revoke credential from organization");
      }
    },

    assignUserToCredential: async (credentialId, userId) => {
      set({ isLoading: true, error: null });
      try {
        await api.post(`/clickhouse-credentials/${credentialId}/users`, {
          userId,
        });
        await get().fetchCredentials();
        toast.success("User assigned to credential successfully");
      } catch (error) {
        set({ error: "Failed to assign user to credential", isLoading: false });
        toast.error("Failed to assign user to credential");
      }
    },

    revokeUserFromCredential: async (credentialId, userId) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/users/${userId}`
        );
        await get().fetchCredentials();
        toast.success("User revoked from credential successfully");
      } catch (error) {
        set({
          error: "Failed to revoke user from credential",
          isLoading: false,
        });
        toast.error("Failed to revoke user from credential");
      }
    },

    setSelectedCredential: (credential) => {
      set({ selectedCredential: credential });
    },
  })
);

export default useClickHouseCredentialStore;
