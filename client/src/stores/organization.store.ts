// organization store
import { create } from "zustand";
import api from "@/api/axios.config";
import { toast } from "sonner";

import { OrganizationState } from "@/types/types";

const useOrganizationStore = create<OrganizationState>((set, get) => ({
  organizations: [],
  selectedOrganization: null,
  isLoading: false,
  error: null,

  fetchOrganizations: async () => {
    set({ isLoading: true, error: null });
    try {
      const response = await api.get("/organizations");
      set({ organizations: response.data, isLoading: false });
    } catch (error) {
      set({ error: "Failed to fetch organizations", isLoading: false });
    }
  },

  setSelectedOrganization: (organization) => {
    set({ selectedOrganization: organization });
  },

  addOrganization: async (name) => {
    set({ isLoading: true, error: null });
    try {
      await api.post("/organizations", { name });
      await get().fetchOrganizations();
      toast.success(`Organization ${name} added successfully`);
    } catch (error) {
      set({ error: "Failed to add organization", isLoading: false });
      toast.error("Failed to add organization");
    }
  },

  updateOrganization: async (id, name) => {
    set({ isLoading: true, error: null });
    try {
      await api.put(`/organizations`, {
        name,
        organizationId: id,
      });
      await get().fetchOrganizations();
      toast.success(`Organization ${name} updated successfully`);
    } catch (error) {
      set({ error: "Failed to update organization", isLoading: false });
      toast.error("Failed to update organization");
    }
  },

  deleteOrganization: async (id) => {
    set({ isLoading: true, error: null });
    try {
      await api.delete(`/organizations`, { data: { organizationId: id } });
      await get().fetchOrganizations();
      toast.info(`Organization deleted successfully`);
    } catch (error) {
      set({ error: "Failed to delete organization", isLoading: false });
      toast.error("Failed to delete organization");
    }
  },

  addUserToOrganization: async (organizationId, userId) => {
    set({ isLoading: true, error: null });
    try {
      console.log(organizationId, userId);
      await api.post(`/organizations/${organizationId}/members`, { userId });
      await get().fetchOrganizations();
      toast.success("User added to organization successfully");
    } catch (error) {
      set({ error: "Failed to add user to organization", isLoading: false });
      toast.error("Failed to add user to organization");
    }
  },

  removeUserFromOrganization: async (organizationId, userId) => {
    set({ isLoading: true, error: null });
    try {
      await api.delete(`/organizations/${organizationId}/members/${userId}`);
      await get().fetchOrganizations();
      toast.success("User removed from organization successfully");
    } catch (error) {
      set({
        error: "Failed to remove user from organization",
        isLoading: false,
      });
      toast.error("Failed to remove user from organization");
    }
  },
}));

export default useOrganizationStore;
