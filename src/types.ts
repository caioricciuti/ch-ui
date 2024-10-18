import { ClickHouseClient } from "@clickhouse/client-web"
import { WebClickHouseClient } from "@clickhouse/client-web/dist/client";

export interface InternalQueries {
  query: string;
  name: string;
  description: string;
  scope: string;
  parameters: string[];
}

export interface Tab {
  id: string;
  title: string;
  type: "sql" | "home" | "information" | "saved_query";
  content: string | { database?: string; table?: string };
  error?: string | null;
  isLoading?: boolean;
  isSaved?: boolean;
  result?: any;
}

export interface Credential {
  host: string;
  username: string;
  password: string;
}

export interface Connection {
  connected: boolean;
  version: string;
}

export interface DatabaseInfo {
  name: string;
  type: string;
  children: { name: string; type: string }[];
}

export interface TablePreviewItem {
  table: string;
  previewData: any[];
}

export interface AppState {
  credential: Credential;
  tabs: Tab[];
  activeTab: string;
  isLoadingCredentials: boolean;
  isServerAvailable: boolean;
  isInitialized: boolean;
  version: string;
  error: string;
  clickHouseClient: WebClickHouseClient | null; // Consider using a more specific type if available
  dataBaseExplorer: DatabaseInfo[];
  isLoadingDatabase: boolean;
  tabError: string | null;
  isTabLoading: boolean;
  isCreateTableModalOpen: boolean;
  isCreateDatabaseModalOpen: boolean;
  selectedDatabaseForCreateTable: string;
  selectedDatabaseForCreateDatabase: string | null;
  selectedTableForCreateTable: string | null;
  selectedTableForCreateDatabase: string | null;
  selectedDatabaseForDelete: string | null;
  selectedTableForDelete: string | null;
  dbInstance: IDBDatabase | null;
  credentialSource: "env" | "app" | null;

  // Methods
  setCredential: (credential: Credential) => Promise<void>;
  clearCredentials: () => Promise<void>;
  checkServerStatus: () => Promise<void>;
  runQuery: (query: string, tabId?: string) => Promise<any>;
  initializeApp: () => Promise<void>;
  fetchDatabaseInfo: () => Promise<void>;
  addTab: (tab: Tab) => Promise<void>;
  updateTab: (tabId: string, updates: Partial<Tab>) => Promise<void>;
  removeTab: (tabId: string) => Promise<void>;
  updateTabTitle: (tabId: string, title: string) => void;
  setActiveTab: (tabId: string) => void;
  closeCreateTableModal: () => void;
  openCreateTableModal: (database: string) => void;
  closeCreateDatabaseModal: () => void;
  openCreateDatabaseModal: () => void;
  getTabById: (tabId: string) => Tab | undefined;
  moveTab: (oldIndex: number, newIndex: number) => void; // Add moveTab here
  setCredentialSource: (source: "env" | "app") => void;
}
