import { create } from "zustand";
import { devtools, persist } from "zustand/middleware";
import api from "../api/axios.config";

interface User {
  _id: string;
  name: string;
  email: string;
  role: "admin" | "user" | "viewer";
  activeOrganization?: string;
  activeClickhouseCredential?: string;
}

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  getCurrentUser: () => Promise<void>;
  setCurrentOrganization: (organizationId: string) => Promise<void>;
  setCurrentCredential: (credentialId: string) => Promise<void>;
}

const useAuthStore = create<AuthState>()(
  devtools(
    persist(
      (set, get) => ({
        user: null,
        isAuthenticated: false,
        isLoading: false,
        error: null,

        login: async (email, password) => {
          set({ isLoading: true, error: null });
          try {
            console.log("Attempting login...");
            await api.post("/auth/login", { email, password });
            console.log("Login successful, fetching user data...");
            await get().getCurrentUser();
            set({ isAuthenticated: true, isLoading: false });
            console.log("Login and user fetch complete");
          } catch (error) {
            console.error("Login failed:", error);
            set({
              error: "Login failed",
              isLoading: false,
              isAuthenticated: false,
            });
          }
        },

        register: async (name, email, password) => {
          set({ isLoading: true, error: null });
          try {
            console.log("Attempting registration...");
            await api.post("/auth/register", { name, email, password });
            set({ isLoading: false });
            console.log("Registration successful");
          } catch (error) {
            console.error("Registration failed:", error);
            set({ error: "Registration failed", isLoading: false });
          }
        },

        logout: async () => {
          set({ isLoading: true, error: null });
          try {
            console.log("Attempting logout...");
            await api.post("/auth/logout");
            set({ user: null, isAuthenticated: false, isLoading: false });
            console.log("Logout successful");
          } catch (error) {
            console.error("Logout failed:", error);
            set({ error: "Logout failed", isLoading: false });
          }
        },

        getCurrentUser: async () => {
          set({ isLoading: true, error: null });
          try {
            console.log("Fetching current user...");
            const response = await api.get("/users/me");
            console.log("Current user data:", response.data);
            set({
              user: response.data,
              isAuthenticated: true,
              isLoading: false,
            });
          } catch (error) {
            console.error("Failed to fetch current user:", error);
            set({
              error: "Failed to fetch current user",
              isLoading: false,
              isAuthenticated: false,
              user: null,
            });
          }
        },

        setCurrentOrganization: async (organizationId) => {
          set({ isLoading: true, error: null });
          try {
            console.log("Setting current organization...");
            await api.post("/users/set-current-organization", {
              organizationId,
            });
            await get().getCurrentUser();
            console.log("Current organization updated");
          } catch (error) {
            console.error("Failed to set current organization:", error);
            set({
              error: "Failed to set current organization",
              isLoading: false,
            });
          }
        },

        setCurrentCredential: async (credentialId) => {
          set({ isLoading: true, error: null });
          try {
            console.log("Setting current credential...");
            await api.post("/users/set-current-credential", { credentialId });
            await get().getCurrentUser();
            console.log("Current credential updated");
          } catch (error) {
            console.error("Failed to set current credential:", error);
            set({
              error: "Failed to set current credential",
              isLoading: false,
            });
          }
        },
      }),
      {
        name: "auth-storage",
        storage: {
          getItem: (name) => {
            const str = localStorage.getItem(name);
            if (!str) return null;
            return JSON.parse(str);
          },
          setItem: (name, value) => {
            localStorage.setItem(name, JSON.stringify(value));
          },
          removeItem: (name) => localStorage.removeItem(name),
        },
      }
    )
  )
);

export default useAuthStore;
