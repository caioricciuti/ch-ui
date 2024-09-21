// organization store
import { create } from "zustand";
import api from "@/api/axios.config";
import { isAxiosError } from "axios";

import { OrganizationState } from "@/types/types";

const useOrganizationStore = create<OrganizationState>((set, get) => ({
  organizations: [],
  selectedOrganization: null,

  fetchOrganizations: async () => {
    try {
      const response = await api.get("/organizations");
      set({ organizations: response.data });
    } catch (error) {
      let msg = "Failed to fetch organizations";
      if (isAxiosError(error)) {
        msg = `${msg}: ${error.response?.data.message}`
      }
      throw new Error(msg)
    }
  },

  setSelectedOrganization: (organization) => {
    set({ selectedOrganization: organization });
  },

  addOrganization: async (name) => {
    try {
      await api.post("/organizations", { name });
      await get().fetchOrganizations();
    } catch (error) {
      let msg = "Failed to add organization"
      if (isAxiosError(error)) {
        msg = `${msg}: ${error.response?.data.message}`
      }
      throw new Error(msg)
    }
  },

  updateOrganization: async (id, name) => {
    try {
      await api.put(`/organizations`, {
        name,
        organizationId: id,
      });
      await get().fetchOrganizations();
    } catch (error) {
      let msg = "Failed to update organization"
      if (isAxiosError(error)) {
        msg = `${msg}: ${error.response?.data.message}`
      }
      throw new Error(msg)
    }
  },

  deleteOrganization: async (id) => {
    try {
      await api.delete(`/organizations`, { data: { organizationId: id } });
      await get().fetchOrganizations();
    } catch (error) {
      let msg = "Failed to delete organization"
      if (isAxiosError(error)) {
        msg = `${msg}: ${error.response?.data.message}`
      }
      throw new Error(msg)
    }
  },

  addUserToOrganization: async (organizationId, userId) => {
    try {
      await api.post(`/organizations/${organizationId}/members`, { userId });
      await get().fetchOrganizations();
    } catch (error) {
      let msg = "Failed to add user to organization"
      if (isAxiosError(error)) {
        msg = `${msg}: ${error.response?.data.message}`
      }
      throw new Error(msg)
    }
  },

  removeUserFromOrganization: async (organizationId, userId) => {
    try {
      await api.delete(`/organizations/${organizationId}/members/${userId}`);
      await get().fetchOrganizations();
    } catch (error) {
      let msg = "Failed to remove user from organization"
      if (isAxiosError(error)) {
        msg = `${msg}: ${error.response?.data.message}`
      }
      throw new Error(msg)
    }
  },
}));

export default useOrganizationStore;
