import { create } from "zustand";
import { devtools } from "zustand/middleware";
import api from "../api/axios.config";
import axios from "axios";

interface Organization {
  _id: string;
  name: string;
  slug: string;
  members: string[];
  owner: string;
  createdAt: string;
  updatedAt: string;
}

interface ClickhouseCredential {
  _id: string;
  name: string;
  slug: string;
  host: string;
  port: number;
  username: string;
  password?: string; // Optional, as you might not always want to include this for security reasons
  owner: string;
  users: Array<string>;
  allowedOrganizations: Array<string>;
  createdAt: Date | string;
  updatedAt: Date | string;
  __v?: number; // Optional, as this is a MongoDB-specific version key
}

interface User {
  _id: string;
  name: string;
  email: string;
  role: "admin" | "user" | "viewer";
  active: boolean;
  createdAt: string;
  updatedAt: string;
  activeOrganization?: Organization;
  activeClickhouseCredential?: ClickhouseCredential;
}

interface AuthState {
  user: User | null;
  allUsers: User[];
  isLoading: boolean;
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  getCurrentUser: () => Promise<void>;
  setCurrentOrganization: (organizationId: string) => Promise<void>;
  setCurrentCredential: (credentialId: string) => Promise<void>;
  checkAuth: () => Promise<boolean>;
  admin: () => boolean;
  updateUser: (userId: string, userData: Partial<User>) => Promise<void>;
  getAllUsers: () => Promise<void>;
}

const useAuthStore = create<AuthState>()(
  devtools((set, get) => ({
    user: null,
    allUsers: [],
    isLoading: false,
    error: null,

    getAllUsers: async () => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.get("/users");
        set({ allUsers: response.data, isLoading: false });
      } catch (error) {
        console.error("Failed to get users:", error);
        set({ error: "Failed to get users", isLoading: false });
        throw error;
      }
    },

    login: async (email, password) => {
      set({ isLoading: true, error: null });
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
              isLoading: false,
            });
          } else {
            set({
              error: error.response.data.message || "Login failed",
              isLoading: false,
            });
          }
        } else {
          set({ error: "An unexpected error occurred", isLoading: false });
        }
        throw error;
      }
    },

    register: async (name, email, password) => {
      set({ isLoading: true, error: null });
      try {
        await api.post("/auth/register", { name, email, password });
        set({ isLoading: false });
      } catch (error) {
        console.error("Registration failed:", error);
        if (axios.isAxiosError(error) && error.response) {
          set({
            error: error.response.data.message || "Registration failed",
            isLoading: false,
          });
        } else {
          set({
            error: "An unexpected error occurred during registration",
            isLoading: false,
          });
        }
        throw error;
      }
    },

    admin: () => get().user?.role === "admin",

    logout: async () => {
      set({ isLoading: true, error: null });
      try {
        await api.post("/auth/logout");
        set({ user: null, allUsers: [], isLoading: false });
        localStorage.removeItem("auth-storage"); // Clear local storage
      } catch (error) {
        console.error("Logout failed:", error);
        set({ error: "Logout failed", isLoading: false });
      }
    },

    updateUser: async (userId, userData) => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.put(`users/me/`, {
          userId,
          ...userData,
        });
        set({ user: response.data, isLoading: false });
      } catch (error) {
        console.error("Failed to update user:", error);
        set({ error: "Failed to update user", isLoading: false });
        throw error;
      }
    },

    getCurrentUser: async () => {
      set({ isLoading: true, error: null });
      try {
        const response = await api.get("/users/me");
        set({ user: response.data, isLoading: false });
      } catch (error) {
        set({ user: null, isLoading: false });
        throw error;
      }
    },

    setCurrentOrganization: async (organizationId) => {
      set({ isLoading: true, error: null });
      try {
        await api.post("/users/me/organization", { organizationId });
        await get().getCurrentUser();
      } catch (error) {
        console.error("Failed to set current organization:", error);
        set({ error: "Failed to set current organization", isLoading: false });
      }
    },

    setCurrentCredential: async (credentialId) => {
      set({ isLoading: true, error: null });
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
          isLoading: false,
        }));
      } catch (error) {
        console.error("Failed to set current credential:", error);
        set({ error: "Failed to set current credential", isLoading: false });
        throw error;
      }
    },

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
