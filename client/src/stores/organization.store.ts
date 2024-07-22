// src/stores/organization.store.ts
import { create } from "zustand";
import api from "@/api/axios.config";
import { toast } from "sonner";

interface Member {
  _id: string;
  name: string;
  email: string;
}

interface Organization {
  _id: string;
  name: string;
  slug: string;
  members: Member[];
  owner: Member;
  createdAt: string;
  updatedAt: string;
}

interface OrganizationState {
  organizations: Organization[];
  selectedOrganization: Organization | null;
  isLoading: boolean;
  error: string | null;
  fetchOrganizations: () => Promise<void>;
  setSelectedOrganization: (organization: Organization) => void;
  addOrganization: (name: string) => Promise<void>;
  updateOrganization: (id: string, name: string) => Promise<void>;
  deleteOrganization: (id: string) => Promise<void>;
}

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
      // Refetch all organizations after adding
      await get().fetchOrganizations();
      toast.success(`Organization ${name} added successfully`);
    } catch (error) {
      set({ error: "Failed to add organization", isLoading: false });
    }
  },

  updateOrganization: async (id, name) => {
    set({ isLoading: true, error: null });
    try {
      const response = await api.put(`/organizations`, {
        name,
        organizationId: id,
      });
      set((state) => ({
        organizations: state.organizations.map((org) =>
          org._id === id ? response.data : org
        ),
        isLoading: false,
      }));
    } catch (error) {
      set({ error: "Failed to update organization", isLoading: false });
    }
  },

  deleteOrganization: async (id) => {
    set({ isLoading: true, error: null });
    try {
      await api.delete(`/organizations`, { data: { organizationId: id } });
      // Refetch all organizations after deleting
      await get().fetchOrganizations();
    } catch (error) {
      set({ error: "Failed to delete organization", isLoading: false });
    }
  },
}));

export default useOrganizationStore;
