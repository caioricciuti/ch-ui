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
    isLoading: false,
    error: null,

    fetchCredentials: async () => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.get<ClickHouseCredential[]>(
          "/clickhouse-credentials"
        );
        set({ credentials: response.data, isLoading: false });
      } catch (error) {
        handleApiError(error, "Failed to fetch credentials", set);
        throw error;
      }
    },

    fetchAvailableCredentials: async (organizationId: string) => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.get<ClickHouseCredential[]>(
          `/clickhouse-credentials/available?organizationId=${organizationId}`
        );
        set({ availableCredentials: response.data, isLoading: false });
      } catch (error) {
        handleApiError(error, "Failed to fetch available credentials", set);
        throw error;
      }
    },

    createCredential: async (credentialData: Partial<ClickHouseCredential>) => {
      set({ isLoading: true, error: null });
      try {
        await api.post("/clickhouse-credentials", credentialData);
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, `Failed to create credential: ${error}`, set);
        throw error;
      }
    },

    updateCredential: async (
      id: string,
      credentialData: Partial<ClickHouseCredential>
    ) => {
      set({ isLoading: true, error: null });
      try {
        await api.put(`/clickhouse-credentials/${id}`, credentialData);
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to update credential", set);
        throw error;
      }
    },

    deleteCredential: async (id: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(`/clickhouse-credentials/${id}`);
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to delete credential", set);
        throw error;
      }
    },

    assignCredentialToOrganization: async (
      credentialId: string,
      organizationId: string
    ) => {
      set({ isLoading: true, error: null });
      try {
        await api.post(
          `/clickhouse-credentials/${credentialId}/organizations`,
          { organizationId }
        );
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(
          error,
          "Failed to assign credential to organization",
          set
        );
        throw error;
      }
    },

    revokeCredentialFromOrganization: async (
      credentialId: string,
      organizationId: string
    ) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/organizations/${organizationId}`
        );
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(
          error,
          "Failed to revoke credential from organization",
          set
        );
        throw error;
      }
    },

    assignUserToCredential: async (credentialId: string, userId: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.post(`/clickhouse-credentials/${credentialId}/users`, {
          userId,
        });
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to assign user to credential", set);
        throw error;
      }
    },

    revokeUserFromCredential: async (credentialId: string, userId: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/users/${userId}`
        );
        await get().fetchCredentials();
      } catch (error) {
        handleApiError(error, "Failed to revoke user from credential", set);
        throw error;
      }
    },

    resetCredentials: () => {
      set({
        availableCredentials: [],
        selectedCredential: null,
        error: null,
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
  set: (state: Partial<ClickHouseCredentialState>) => void
) => {
  if (error instanceof AxiosError && error.response) {
    set({
      error: `${defaultMessage}: ${
        error.response.data.message || error.message
      }`,
      isLoading: false,
    });
  } else {
    set({ error: defaultMessage, isLoading: false });
  }
};

export default useClickHouseCredentialStore;
