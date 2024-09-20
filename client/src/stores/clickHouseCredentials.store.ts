// clickHouseCredentials store
import { create } from "zustand";
import api from "@/api/axios.config";
import { ClickHouseCredentialState, ClickHouseCredential } from "@/types/types";
import { isAxiosError } from "axios";

const useClickHouseCredentialStore = create<ClickHouseCredentialState>(
  (set, get) => ({
    credentials: [],
    selectedCredential: null,
    availableCredentials: [],

    fetchCredentials: async () => {
      try {
        const response = await api.get("/clickhouse-credentials");
        set({ credentials: response.data });
      } catch (error) {
        throw new Error("Failed to fetch credentials")
      }
    },
    // fetch available credentials for the current user based on the organization they are in
    fetchAvailableCredentials: async (organizationId) => {
      try {
        const response = await api.get(
          `/clickhouse-credentials/available?organizationId=${organizationId}`
        );
        set({ availableCredentials: response.data });
      } catch (error) {
        throw new Error("Failed to fetch available credentials")
      }
    },

    createCredential: async (credentialData) => {
      try {
        await api.post("/clickhouse-credentials", credentialData);
        await get().fetchCredentials();
      } catch (error) {
        let msg = 'Failed to create credential'
        if (isAxiosError(error)) {
          msg = `${msg}: ${error.response?.data.message}`
        }
        throw new Error(msg)
      }
    },

    updateCredential: async (id, credentialData) => {
      try {
        await api.put(`/clickhouse-credentials/${id}`, credentialData);
        await get().fetchCredentials();
      } catch (error) {
        let msg = "Failed to update credential"
        if (isAxiosError(error)) {
          msg = `${msg}: ${error.response?.data.message}`
        }
        throw new Error(msg)
      }
    },

    deleteCredential: async (id) => {
      try {
        await api.delete(`/clickhouse-credentials/${id}`);
        await get().fetchCredentials();
      } catch (error) {
        let msg = "Failed to delete credential"
        if (isAxiosError(error)) {
          msg = `${msg}: ${error.response?.data.message}`
        }
        throw new Error(msg)
      }
    },

    assignCredentialToOrganization: async (credentialId, organizationId) => {
      try {
        await api.post(
          `/clickhouse-credentials/${credentialId}/organizations`,
          { organizationId }
        );
        await get().fetchCredentials();
      } catch (error) {
        let msg = "Failed to assign credential to organization";
        if (isAxiosError(error)) {
          msg = `${msg}: ${error.response?.data.message}`
        }
        throw new Error(msg)
      }
    },

    revokeCredentialFromOrganization: async (credentialId, organizationId) => {
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/organizations/${organizationId}`
        );
        await get().fetchCredentials();
      } catch (error) {
        let msg = "Failed to revoke credential from organization"
        if (isAxiosError(error)) {
          if (isAxiosError(error)) {
            msg = `${msg}: ${error.response?.data.message}`
          }
          throw new Error(msg)
        }
      }
    },

    assignUserToCredential: async (credentialId, userId) => {
      try {
        await api.post(`/clickhouse-credentials/${credentialId}/users`, {
          userId,
        });
        await get().fetchCredentials();
      } catch (error) {
        let msg = "Failed to assign user to credential"
        if (isAxiosError(error)) {
          if (isAxiosError(error)) {
            msg = `${msg}: ${error.response?.data.message}`
          }
          throw new Error(msg)
        }
      }
    },

    revokeUserFromCredential: async (credentialId, userId) => {
      try {
        await api.delete(
          `/clickhouse-credentials/${credentialId}/users/${userId}`
        );
        await get().fetchCredentials();
      } catch (error) {
        let msg = "Failed to revoke user from credential"
        if (isAxiosError(error)) {
          if (isAxiosError(error)) {
            msg = `${msg}: ${error.response?.data.message}`
          }
          throw new Error(msg)
        }
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
  })
);

export default useClickHouseCredentialStore;
