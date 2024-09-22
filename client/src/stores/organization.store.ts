import { create } from "zustand";
import { devtools } from "zustand/middleware";
import api from "@/api/axios.config";
import { AxiosError } from "axios";
import { OrganizationState, Organization } from "@/types/types";

const useOrganizationStore = create<OrganizationState>()(
  devtools((set, get) => ({
    organizations: [],
    selectedOrganization: null,
    isLoading: false,
    error: null,

    fetchOrganizations: async () => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.get<Organization[]>("/organizations");
        set({ organizations: response.data, isLoading: false });
      } catch (error) {
        handleApiError(error, "Failed to fetch organizations", set);
        throw error;
      }
    },

    setSelectedOrganization: (organization: Organization | null) => {
      set({ selectedOrganization: organization });
    },

    addOrganization: async (name: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.post("/organizations", { name });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to add organization", set);
        throw error;
      }
    },

    updateOrganization: async (id: string, name: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.put("/organizations", {
          name,
          organizationId: id,
        });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to update organization", set);
        throw error;
      }
    },

    deleteOrganization: async (id: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete("/organizations", { data: { organizationId: id } });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to delete organization", set);
        throw error;
      }
    },

    addUserToOrganization: async (organizationId: string, userId: string) => {
      set({ isLoading: true, error: null });
      try {
        await api.post(`/organizations/${organizationId}/members`, { userId });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to add user to organization", set);
        throw error;
      }
    },

    removeUserFromOrganization: async (
      organizationId: string,
      userId: string
    ) => {
      set({ isLoading: true, error: null });
      try {
        await api.delete(`/organizations/${organizationId}/members/${userId}`);
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to remove user from organization", set);
        throw error;
      }
    },
  }))
);

const handleApiError = (
  error: unknown,
  defaultMessage: string,
  set: (state: Partial<OrganizationState>) => void
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

export default useOrganizationStore;
