// src/types/common.ts
import {
  ClickHouseSettings as ClickHouseSettingsType,
  ClickHouseClient,
} from "@clickhouse/client-web";
export type ClickHouseSettings = ClickHouseSettingsType;

export interface Credential {
  url: string;
  username: string;
  password: string;
  requestTimeout: number;
  useAdvanced: boolean;
  customPath: string;
  isDistributed?: boolean;
  clusterName?: string;
}

export interface DatabaseInfo {
  name: string;
  type: string;
  children: { name: string; type: string }[];
}

interface Tab {
  id: string;
  title: string;
  type: "sql" | "home" | "information";
  content: string | { database?: string; table?: string };
  error?: string | null;
  isLoading?: boolean;
  isSaved?: boolean;
  result?: any;
  isDirty?: boolean;
}

export interface SavedQuery {
  id: string;
  name: string;
  query: string;
  created_at: string;
  updated_at: string;
  owner: string;
  is_public: boolean;
}

interface SavedQueriesState {
  isSavedQueriesActive: boolean;
  isCheckingStatus: boolean;
  isActivating: boolean;
  isDeactivating: boolean;
  error: string | null;
}

export interface QueryResult {
  meta: any[];
  data: any[];
  statistics: {
    elapsed: number;
    rows_read: number;
    bytes_read: number;
  };
  rows?: number;
  error?: string | null;
}

interface CoreState {
  credential: Credential;
  clickHouseClient: ClickHouseClient | null;
  isLoadingCredentials: boolean;
  isServerAvailable: boolean;
  isInitialized: boolean;
  version: string;
  error: string;
  credentialSource: "env" | "app" | null;
  clickhouseSettings: ClickHouseSettings;
}

interface WorkspaceState {
  tabs: Tab[];
  activeTab: string;
  tabError: string | null;
  isTabLoading: boolean;
}

interface ExplorerState {
  dataBaseExplorer: DatabaseInfo[];
  isLoadingDatabase: boolean;
  isCreateTableModalOpen: boolean;
  isCreateDatabaseModalOpen: boolean;
  selectedDatabaseForCreateTable: string;
  selectedDatabaseForCreateDatabase: string | null;
  selectedTableForCreateTable: string | null;
  selectedTableForCreateDatabase: string | null;
  selectedDatabaseForDelete: string | null;
  selectedTableForDelete: string | null;
  isUploadFileModalOpen: boolean;
  selectedDatabaseForUpload: string;
}

interface AdminState {
  isAdmin: boolean;
  savedQueries: SavedQueriesState;
}

export interface AppState
  extends CoreState,
    WorkspaceState,
    ExplorerState,
    AdminState {
  setCredential: (credential: Credential) => Promise<void>;
  clearCredentials: () => Promise<void>;
  checkServerStatus: () => Promise<void>;
  runQuery: (query: string, tabId?: string) => Promise<QueryResult>;
  initializeApp: () => Promise<void>;
  setCredentialSource: (source: "env" | "app") => void;
  updateConfiguration: (clickhouseSettings: ClickHouseSettings) => void;
  updatedSavedQueriesTrigger: string;
  addTab: (tab: Tab) => Promise<void>;
  updateTab: (tabId: string, updates: Partial<Tab>) => Promise<void>;
  removeTab: (tabId: string) => Promise<void>;
  updateTabTitle: (tabId: string, title: string) => Promise<void>;
  setActiveTab: (tabId: string) => void;
  getTabById: (tabId: string) => Tab | undefined;
  moveTab: (oldIndex: number, newIndex: number) => void;
  duplicateTab: (tabId: string) => void | Promise<void>;
  closeAllTabs: () => void | Promise<void>;

  fetchDatabaseInfo: () => Promise<void>;
  closeCreateTableModal: () => void;
  openCreateTableModal: (database: string) => void;
  closeCreateDatabaseModal: () => void;
  openCreateDatabaseModal: () => void;
  closeUploadFileModal: () => void;
  openUploadFileModal: (database: string) => void;

  checkIsAdmin: () => Promise<boolean>;
  activateSavedQueries: () => Promise<void>;
  deactivateSavedQueries: () => Promise<boolean>;
  checkSavedQueriesStatus: () => Promise<boolean>;

  saveQuery: (tabId: string, queryName: string, query: string) => Promise<void>;
  updateSavedQuery: (
    tabId: string,
    queryName: string,
    query: string
  ) => Promise<void>;
  fetchSavedQueries: (id?: string) => Promise<Array<any>>;
  deleteSavedQuery: (id: string) => Promise<void>;

  // Utilities
  clearLocalData: () => void;
}
