// src/types/common.ts
import { ClickHouseSettings } from "@clickhouse/client-web";
import { WebClickHouseClient } from "@clickhouse/client-web/dist/client";

export interface Credential {
    url: string;
    username: string;
    password: string;
    useAdvanced: boolean;
    customPath: string;
}

export interface DatabaseInfo {
    name: string;
    type: string;
    children: { name: string; type: string }[];
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

export interface SavedQueriesState {
    isSavedQueriesActive: boolean;
    isCheckingStatus: boolean;
    isActivating: boolean;
    isDeactivating: boolean;
    error: string | null;
}

// src/types/store/slices.ts
export interface CoreState {
    credential: Credential;
    clickHouseClient: WebClickHouseClient | null;
    isLoadingCredentials: boolean;
    isServerAvailable: boolean;
    isInitialized: boolean;
    version: string;
    error: string;
    credentialSource: "env" | "app" | null;
    clickhouseSettings: ClickHouseSettings
}

export interface CoreSlice extends CoreState {
    setCredential: (credential: Credential) => Promise<void>;
    clearCredentials: () => Promise<void>;
    checkServerStatus: () => Promise<void>;
    runQuery: (query: string, tabId?: string) => Promise<any>;
    initializeApp: () => Promise<void>;
    setCredentialSource: (source: "env" | "app") => void;
    updateConfiguration: (clickhouseSettings: ClickHouseSettings) => void;
}

export interface WorkspaceState {
    tabs: Tab[];
    activeTab: string;
    tabError: string | null;
    isTabLoading: boolean;
    indexDbInstance: IDBDatabase | null;
}

export interface WorkspaceSlice extends WorkspaceState {
    addTab: (tab: Tab) => Promise<void>;
    updateTab: (tabId: string, updates: Partial<Tab>) => Promise<void>;
    removeTab: (tabId: string) => Promise<void>;
    updateTabTitle: (tabId: string, title: string) => Promise<void>;
    setActiveTab: (tabId: string) => void;
    getTabById: (tabId: string) => Tab | undefined;
    moveTab: (oldIndex: number, newIndex: number) => void;
}

export interface ExplorerState {
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
}

export interface ExplorerSlice extends ExplorerState {
    fetchDatabaseInfo: () => Promise<void>;
    closeCreateTableModal: () => void;
    openCreateTableModal: (database: string) => void;
    closeCreateDatabaseModal: () => void;
    openCreateDatabaseModal: () => void;
}

export interface AdminState {
    isAdmin: boolean;
    savedQueries: SavedQueriesState;
}

export interface AdminSlice extends AdminState {
    checkIsAdmin: () => Promise<boolean>;
    activateSavedQueries: () => Promise<void>;
    deactivateSavedQueries: () => Promise<boolean>;
    checkSavedQueriesStatus: () => Promise<boolean>;
}

// src/types/store/index.ts
export interface AppState extends
    CoreSlice,
    WorkspaceSlice,
    ExplorerSlice,
    AdminSlice { }

export interface QueryResult {
    meta: any[];
    data: any[];
    statistics: {
        elapsed: number;
        rows_read: number;
        bytes_read: number;
    };
    rows?: number;
}