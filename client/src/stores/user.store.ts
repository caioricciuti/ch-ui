import { create } from "zustand";
import { devtools, persist, PersistOptions, StorageValue } from "zustand/middleware";
import api from "../api/axios.config";
import axios from "axios";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import { AuthState, User } from "@/types/types";

// Custom storage object
const customStorage: PersistOptions<AuthState>["storage"] = {
  getItem: (name): StorageValue<AuthState> | Promise<StorageValue<AuthState> | null> | null => {
    const str = localStorage.getItem(name);
    if (str) return JSON.parse(str) as AuthState;
    return null;
  },
  setItem: (name, value): void => {
    localStorage.setItem(name, JSON.stringify(value));
  },
  removeItem: (name): void => {
    localStorage.removeItem(name);
  },
};

const useAuthStore = create<AuthState>()(
  devtools(
    persist(
      (set, get) => {
        const handleAuthError = (error: unknown, defaultMessage: string) => {
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
                error: error.response.data.message || defaultMessage,
                authIsLoading: false,
              });
            }
          } else {
            set({
              error: "An unexpected error occurred",
              authIsLoading: false,
            });
          }
        };

        return {
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
              set({ error: "Failed to get users", authIsLoading: false });
              throw error;
            }
          },

          login: async (email: string, password: string) => {
            set({ authIsLoading: true, error: null });
            try {
              await api.post("/auth/login", { email, password });
              await get().getCurrentUser();
            } catch (error) {
              handleAuthError(error, "Login failed");
              throw error;
            }
          },

          register: async (name: string, email: string, password: string) => {
            set({ authIsLoading: true, error: null });
            try {
              await api.post("/auth/register", { name, email, password });
              set({ authIsLoading: false });
            } catch (error) {
              handleAuthError(error, "Registration failed");
              throw error;
            }
          },

          admin: () => get().user?.role === "admin",

          logout: async () => {
            set({ authIsLoading: true, error: null });
            try {
              await api.post("/auth/logout");
              set({ user: null, allUsers: [], authIsLoading: false });
            } catch (error) {
              set({ error: "Logout failed", authIsLoading: false });
            }
          },

          updateUser: async (userId: string, userData: Partial<User>) => {
            set({ authIsLoading: true, error: null });
            try {
              const response = await api.put(`users/me/`, {
                userId,
                ...userData,
              });
              set({ user: response.data, authIsLoading: false });
            } catch (error) {
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

          setCurrentOrganization: async (organizationId: string) => {
            set({ authIsLoading: true, error: null });
            try {
              await api.post("/users/me/organization", { organizationId });
              const response = await api.get("/users/me");
              await api.post("/users/me/credential", { credentialId: "" });
              set({ user: response.data, authIsLoading: false });

              const credentialStore = useClickHouseCredentialStore.getState();
              credentialStore.resetCredentials();
              await credentialStore.fetchAvailableCredentials(organizationId);
            } catch (error) {
              set({
                error: "Failed to set current organization",
                authIsLoading: false,
              });
              throw error;
            }
          },

          setCurrentCredential: async (credentialId: string) => {
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
              set({
                error: "Failed to set current credential",
                authIsLoading: false,
              });
              throw error;
            }
          },

          getActiveOrganization: () => get().user?.activeOrganization || null,
          getActiveCredential: () =>
            get().user?.activeClickhouseCredential || null,

          checkAuth: async () => {
            try {
              await get().getCurrentUser();
              return true;
            } catch (error) {
              return false;
            }
          },
        };
      },
      {
        name: "auth-storage",
        storage: customStorage,
      }
    )
  )
);

export default useAuthStore;
