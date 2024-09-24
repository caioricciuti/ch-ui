import { create } from "zustand";
import { devtools } from "zustand/middleware";
import api from "@/api/axios.config";
import { AxiosError } from "axios";
import { OrganizationState, Organization } from "@/types/types";

const useOrganizationStore = create<OrganizationState>()(
  devtools((set, get) => ({
    organizations: [],
    selectedOrganization: null,

    fetchOrganizations: async () => {
      try {
        const response = await api.get<Organization[]>("/organizations");
        set({ organizations: response.data });
      } catch (error) {
        handleApiError(error, "Failed to fetch organizations");
      }
    },

    setSelectedOrganization: (organization: Organization | null) => {
      set({ selectedOrganization: organization });
    },

    addOrganization: async (name: string) => {
      try {
        await api.post("/organizations", { name });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to add organization");
      }
    },

    updateOrganization: async (id: string, name: string) => {
      try {
        await api.put("/organizations", {
          name,
          organizationId: id,
        });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to update organization");
      }
    },

    deleteOrganization: async (id: string) => {
      try {
        await api.delete("/organizations", { data: { organizationId: id } });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to delete organization");
      }
    },

    addUserToOrganization: async (organizationId: string, userId: string) => {
      try {
        await api.post(`/organizations/${organizationId}/members`, { userId });
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to add user to organization");
      }
    },

    removeUserFromOrganization: async (
      organizationId: string,
      userId: string
    ) => {
      try {
        await api.delete(`/organizations/${organizationId}/members/${userId}`);
        await get().fetchOrganizations();
      } catch (error) {
        handleApiError(error, "Failed to remove user from organization");
      }
    },
  }))
);

const handleApiError = (
  error: unknown,
  defaultMessage: string,
): Error => {
  if (error instanceof AxiosError && error.response) {
    throw new Error(`${defaultMessage}: ${error.response.data.message || error.message}`)
  } else {
    throw new Error(defaultMessage)
  }
};

export default useOrganizationStore;
