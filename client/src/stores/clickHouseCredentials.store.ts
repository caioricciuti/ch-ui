import { create } from "zustand";
import { devtools } from "zustand/middleware";
import api from "@/api/axios.config";
import { AxiosError } from "axios";
import { ClickHouseCredentialState, ClickHouseCredential } from "@/types/types";

const useClickHouseCredentialStore = create<ClickHouseCredentialState>()(
  devtools((set, get) => ({
    credentials: [],
    selectedCredential: null,
    availableCredentials: [],

    fetchCredentials: async () => {
      try {
        const response = await api.get<ClickHouseCredential[]>(
          "/clickhouse-credentials"
        );
        set({ credentials: response.data })
      } catch (error) {
        handleApiError(error, "Failed to fetch credentials");
      }
    },

    fetchAvailableCredentials: async (organizationId: string) => {
      try {
        const response = await api.get<ClickHouseCredential[]>(
          `/clickhouse-credentials/available?organizationId=${organizationId}`
        );
        set({ availableCredentials: response.data });
      } catch (error) {
        handleApiError(error, "Failed to fetch available credentials");
      }
    },

    createCredential: async (credentialData: Partial<ClickHouseCredential>) => {
      try {
        await api.post("/clickhouse-credentials", credentialData);
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, `Failed to create credential: ${error}`);
      }
    },

    updateCredential: async (
      id: string,
      credentialData: Partial<ClickHouseCredential>
    ) => {
      try {
        await api.put(`/clickhouse-credentials/${id}`, credentialData);
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to update credential");
      }
    },

    deleteCredential: async (id: string) => {
      try {
        await api.delete(`/clickhouse-credentials/${id}`);
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to delete credential");
      }
    },

    assignCredentialToOrganization: async (
      credentialId: string,
      organizationId: string
    ) => {
      try {
        await api.post(
          `/clickhouse-credentials/${credentialId}/organizations`,
          { organizationId }
        );
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(
          error,
          "Failed to assign credential to organization"        );
      }
    },

    revokeCredentialFromOrganization: async (
      credentialId: string,
      organizationId: string
    ) => {
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/organizations/${organizationId}`
        );
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(
          error,
          "Failed to revoke credential from organization",
        );
      }
    },

    assignUserToCredential: async (credentialId: string, userId: string) => {
      try {
        await api.post(`/clickhouse-credentials/${credentialId}/users`, {
          userId,
        });
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to assign user to credential");
      }
    },

    revokeUserFromCredential: async (credentialId: string, userId: string) => {
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/users/${userId}`
        );
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to revoke user from credential");
      }
    },

    resetCredentials: () => {
      set({
        availableCredentials: [],
        selectedCredential: null,
      });
    },

    setSelectedCredential: (credential: ClickHouseCredential | null) => {
      set({ selectedCredential: credential });
    },
  }))
);

const handleApiError = (
  error: unknown,
  defaultMessage: string,
) => {
  if (error instanceof AxiosError && error.response) {
    throw new Error(`${defaultMessage}: ${error.response.data.message || error.message}`)
  } else {
    throw new Error(`${defaultMessage}`)
  }
};

export default useClickHouseCredentialStore;
