// USER AND AUTH TYPES

export interface AuthState {
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

export interface User {
  _id: string;
  name: string;
  email: string;
  role: "admin" | "user" | "viewer";
  active: boolean;
  createdAt: string;
  updatedAt: string;
  activeOrganization?: Organization;
  activeClickhouseCredential?: ClickHouseCredential;
}

// ORGANIZATION

export interface OrganizationState {
  organizations: Organization[];
  selectedOrganization: Organization | null;
  isLoading: boolean;
  error: string | null;
  fetchOrganizations: () => Promise<void>;
  setSelectedOrganization: (organization: Organization) => void;
  addOrganization: (name: string) => Promise<void>;
  updateOrganization: (id: string, name: string) => Promise<void>;
  deleteOrganization: (id: string) => Promise<void>;
  addUserToOrganization: (
    organizationId: string,
    userId: string
  ) => Promise<void>;
  removeUserFromOrganization: (
    organizationId: string,
    userId: string
  ) => Promise<void>;
}

export interface Organization {
  _id: string;
  name: string;
  slug: string;
  members: User[]; // Changed from Member[] to User[]
  owner: User;
  createdAt: string;
  updatedAt: string;
}

// CLICKHOUSE CREDENTIALS

export interface ClickHouseCredentialState {
  credentials: ClickHouseCredential[];
  selectedCredential: ClickHouseCredential | null;
  availableCredentials: ClickHouseCredential[];
  isLoading: boolean;
  error: string | null;
  fetchCredentials: () => Promise<void>;
  fetchAvailableCredentials: () => Promise<void>;
  createCredential: (
    credentialData: Partial<ClickHouseCredential>
  ) => Promise<void>;
  updateCredential: (
    id: string,
    credentialData: Partial<ClickHouseCredential>
  ) => Promise<void>;
  deleteCredential: (id: string) => Promise<void>;
  assignCredentialToOrganization: (
    credentialId: string,
    organizationId: string
  ) => Promise<void>;
  revokeCredentialFromOrganization: (
    credentialId: string,
    organizationId: string
  ) => Promise<void>;
  assignUserToCredential: (
    credentialId: string,
    userId: string
  ) => Promise<void>;
  revokeUserFromCredential: (
    credentialId: string,
    userId: string
  ) => Promise<void>;
  setSelectedCredential: (credential: ClickHouseCredential | null) => void;
  resetCredentials: () => void;
}

export interface ClickHouseCredential {
  _id: string;
  name: string;
  slug: string;
  host: string;
  port: number;
  username: string;
  password?: string;
  owner: string;
  users: Array<string>;
  allowedOrganizations: Array<string>;
  createdAt: Date | string;
  updatedAt: Date | string;
  __v?: number;
}

// Tab related types
export type TabType = "sql" | "result" | "home" | "information";

export interface Tab {
  id: string;
  title: string;
  content: string | { query: string; database: string; table: string };
  type: TabType;
  results?: any[]; // Query results
  error?: string | null; // Query error
  isLoading?: boolean; // Loading state for the query
}

export interface TabQueryState {
  tabs: Tab[];
  activeTabId: string;
  isLoading: boolean;
  error: string | null;

  addTab: (tab: Omit<Tab, "id">) => void;
  closeTab: (id: string) => void;
  updateTabContent: (id: string, updatedValues: Partial<Tab>) => void;
  setActiveTab: (id: string) => void;
  updateTabTitle: (id: string, title: string) => void;
  moveTab: (fromIndex: number, toIndex: number) => void;
  getTabById: (id: string) => Tab | undefined;
  fetchQueries: () => Promise<void>;
  runQuery: (tabId: string, query: string) => Promise<void>;
}
