// user.store.ts
import { create } from "zustand";
import { devtools } from "zustand/middleware";
import api from "../api/axios.config";
import axios from "axios";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import { AuthState } from "@/types/types";

const useAuthStore = create<AuthState>()(
  devtools((set, get) => ({
    user: null,
    allUsers: [],
    authIsLoading: false,
    error: null,

    getAllUsers: async () => {
      set({ authIsLoading: true, error: null });
      try {
        const response = await api.get("/users");
        set({ allUsers: response.data, authIsLoading: false });
      } catch (error) {
        console.error("Failed to get users:", error);
        set({ error: "Failed to get users", authIsLoading: false });
        throw error;
      }
    },

    login: async (email, password) => {
      set({ authIsLoading: true, error: null });
      try {
        await api.post("/auth/login", { email, password });
        await get().getCurrentUser();
      } catch (error) {
        console.error("Login failed, store: ", error);
        if (axios.isAxiosError(error) && error.response) {
          if (
            error.response.status === 403 &&
            error.response.data.internalCode === 1020
          ) {
            set({
              error:
                "Account not activated. Please check your email to activate your account.",
              authIsLoading: false,
            });
          } else {
            set({
              error: error.response.data.message || "Login failed",
              authIsLoading: false,
            });
          }
        } else {
          set({ error: "An unexpected error occurred", authIsLoading: false });
        }
        throw error;
      }
    },

    register: async (name, email, password) => {
      set({ authIsLoading: true, error: null });
      try {
        await api.post("/auth/register", { name, email, password });
        set({ authIsLoading: false });
      } catch (error) {
        console.error("Registration failed:", error);
        if (axios.isAxiosError(error) && error.response) {
          set({
            error: error.response.data.message || "Registration failed",
            authIsLoading: false,
          });
        } else {
          set({
            error: "An unexpected error occurred during registration",
            authIsLoading: false,
          });
        }
        throw error;
      }
    },

    admin: () => get().user?.role === "admin",

    logout: async () => {
      set({ authIsLoading: true, error: null });
      try {
        await api.post("/auth/logout");
        set({ user: null, allUsers: [], authIsLoading: false });
        localStorage.removeItem("auth-storage"); // Clear local storage
      } catch (error) {
        console.error("Logout failed:", error);
        set({ error: "Logout failed", authIsLoading: false });
      }
    },

    updateUser: async (userId, userData) => {
      set({ authIsLoading: true, error: null });
      try {
        const response = await api.put(`users/me/`, {
          userId,
          ...userData,
        });
        set({ user: response.data, authIsLoading: false });
      } catch (error) {
        console.error("Failed to update user:", error);
        set({ error: "Failed to update user", authIsLoading: false });
        throw error;
      }
    },

    getCurrentUser: async () => {
      set({ authIsLoading: true, error: null });
      try {
        const response = await api.get("/users/me");
        set({ user: response.data, authIsLoading: false });
      } catch (error) {
        set({ user: null, authIsLoading: false });
        throw error;
      }
    },

    setCurrentOrganization: async (organizationId) => {
      set({ authIsLoading: true, error: null });
      try {
        await api.post("/users/me/organization", { organizationId });
        const response = await api.get("/users/me");

        // reset the user selected credential
        await api.post("/users/me/credential", {
          credentialId: "",
        });

        set({ user: response.data, authIsLoading: false });
        // Reset and fetch available credentials
        const credentialStore = useClickHouseCredentialStore.getState();
        credentialStore.resetCredentials();
        await credentialStore.fetchAvailableCredentials(organizationId);
      } catch (error) {
        console.error("Failed to set current organization:", error);
        set({
          error: "Failed to set current organization",
          authIsLoading: false,
        });
      }
    },

    setCurrentCredential: async (credentialId) => {
      set({ authIsLoading: true, error: null });
      try {
        const response = await api.post("/users/me/credential", {
          credentialId,
        });
        set((state) => ({
          user: state.user
            ? {
                ...state.user,
                activeClickhouseCredential:
                  response.data.activeClickhouseCredential,
              }
            : null,
          authIsLoading: false,
        }));
      } catch (error) {
        console.error("Failed to set current credential:", error);
        set({
          error: "Failed to set current credential",
          authIsLoading: false,
        });
        throw error;
      }
    },

    getActiveOrganization: () => get().user?.activeOrganization || null,
    getActiveCredential: () => get().user?.activeClickhouseCredential || null,

    checkAuth: async () => {
      try {
        await get().getCurrentUser();
        return true;
      } catch (error) {
        return false;
      }
    },
  }))
);

export default useAuthStore;
