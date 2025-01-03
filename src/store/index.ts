// src/store/store.ts
import { create } from "zustand";
import { persist } from "zustand/middleware";
import {
  AppState,
  Credential,
  DatabaseInfo,
  ClickHouseSettings,
  QueryResult,
} from "@/types/common";
import { createClient, ClickHouseClient } from "@clickhouse/client-web";
import { isCreateOrInsert } from "@/helpers/sqlUtils";
import * as IndexedDB from "@/lib/indexDB";
import { OverflowMode } from "@clickhouse/client-common/dist/settings";
import { toast } from "sonner";
import { appQueries } from "@/features/workspace/editor/appQueries";
// Define specific error types
export class ClickHouseError extends Error {
  constructor(message: string, public readonly originalError?: unknown) {
    super(message);
    this.name = "ClickHouseError";
  }
}

interface AdminCheckResponse {
  data: Array<{ is_admin: boolean }>;
}

interface SavedQueriesCheckResponse {
  data: Array<{ exists: number }>;
}

const buildConnectionUrl = (credential: Credential): string => {
  let baseUrl = credential.url;
  baseUrl = baseUrl.replace(/\/+$/, "");

  if (credential.useAdvanced && credential.customPath) {
    const cleanPath = credential.customPath.replace(/^\/+/, "");
    return `${baseUrl}/${cleanPath}`;
  }
  return baseUrl;
};

const useAppStore = create<AppState>()(
  persist(
    (set, get) => ({
      // Core State & Actions
      credential: {
        url: "",
        username: "",
        password: "",
        useAdvanced: false,
        customPath: "",
      },
      clickHouseClient: null,
      isLoadingCredentials: false,
      isServerAvailable: false,
      isInitialized: false,
      version: "",
      error: "",
      credentialSource: null,
      clickhouseSettings: {
        max_result_rows: "0",
        max_result_bytes: "0",
        result_overflow_mode: 'throw' as OverflowMode,
      },
      setCredentialSource: (source) => set({ credentialSource: source }),
      setCredential: async (credential) => {
        set({ credential, isLoadingCredentials: true });
        try {
          const connectionUrl = buildConnectionUrl(credential);
          const client = createClient({
            url: connectionUrl,
            pathname: credential.customPath, // Use custom path for proxy
            username: credential.username,
            password: credential.password || "",
            clickhouse_settings: {
              ...get().clickhouseSettings,
              result_overflow_mode: "throw",
            },
          });
          set({ clickHouseClient: client });
          await get()
            .checkServerStatus()
            .then(() => {
              get().checkIsAdmin();
            });
        } catch (error) {
          set({ error: (error as Error).message });
          toast.error(`Failed to set credentials: ${(error as Error).message}`);
        } finally {
          set({ isLoadingCredentials: false });
        }
      },
      updateConfiguration: async (clickhouseSettings: ClickHouseSettings) => {
        try {
          const credentials = get().credential;
          const connectionUrl = buildConnectionUrl(credentials);
          const client = createClient({
            url: connectionUrl,
            pathname: credentials.customPath, // Ensure custom path is applied
            username: credentials.username,
            password: credentials.password || "",
            clickhouse_settings: clickhouseSettings,
          });

          set({ clickHouseClient: client, clickhouseSettings });
          await get().checkServerStatus();
        } catch (error) {
          throw error;
        }
      },
      clearCredentials: async () => {
        set({
          credential: {
            url: "",
            username: "",
            password: "",
            useAdvanced: false,
            customPath: "",
          },
          clickhouseSettings: {
            max_result_rows: "0",
            max_result_bytes: "0",
            result_overflow_mode: "throw",
          },
          clickHouseClient: null,
          isServerAvailable: false,
          version: "",
          error: "",
        });
      },
      checkServerStatus: async () => {
        const { clickHouseClient } = get();
        set({ isLoadingCredentials: true, error: "" });
        try {
          if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
          }
          await clickHouseClient.ping();
          const versionResult = await clickHouseClient.query({
            query: "SELECT version()",
          });
          const versionData = (await versionResult.json()) as {
            data: { "version()": string }[];
          };
          const version = versionData.data[0]["version()"];
          set({ isServerAvailable: true, version });
        } catch (error: any) {
          let errorMessage = error.message;
          if (error.response?.status === 404) {
            errorMessage =
              "Unable to connect to the specified cluster. Please check your custom path configuration.";
          } else if (error.response?.status === 502) {
            errorMessage =
              "Proxy error: Unable to reach the ClickHouse server. Please verify your cluster is running.";
          }
          set({ isServerAvailable: false, error: errorMessage });
          // Clear credentials when connection fails
          await get().clearCredentials();
          toast.error("Connection failed. Credentials have been cleared.");
        } finally {
          set({ isLoadingCredentials: false });
        }
      },

      runQuery: async (query: string, tabId?: string) => {
        const { clickHouseClient } = get();
        if (!clickHouseClient) {
          throw new Error("ClickHouse client is not initialized");
        }
        if (tabId) {
          set((state) => ({
            tabs: state.tabs.map((tab) =>
              tab.id === tabId ? { ...tab, isLoading: true, error: null } : tab
            ),
          }));
        }
        try {
          const trimmedQuery = query.trim();

          if (isCreateOrInsert(trimmedQuery)) {
            await clickHouseClient.command({ query: trimmedQuery });
            const result: QueryResult = {
              meta: [],
              data: [],
              statistics: {
                elapsed: 0,
                rows_read: 0,
                bytes_read: 0,
              },
              rows: 0,
              error: null,
            };

            if (tabId)
              await get().updateTab(tabId, {
                result,
                isLoading: false,
                error: null,
              });
            return result;
          }
          const result = await clickHouseClient.query({ query: trimmedQuery });
          const jsonResult = (await result.json()) as any;
          const processedResult: QueryResult = {
            meta: jsonResult.meta || [],
            data: jsonResult.data || [],
            statistics: jsonResult.statistics || {
              elapsed: 0,
              rows_read: 0,
              bytes_read: 0,
            },
            rows: jsonResult.rows || 0,
            error: null,
          };
          if (tabId)
            await get().updateTab(tabId, {
              result: processedResult,
              isLoading: false,
            });
          return processedResult;
        } catch (error: any) {
          const errorResult: QueryResult = {
            meta: [],
            data: [],
            statistics: {
              elapsed: 0,
              rows_read: 0,
              bytes_read: 0,
            },
            rows: 0,
            error: error.message,
          };
          if (tabId)
            await get().updateTab(tabId, {
              result: errorResult,
              isLoading: false,
              error: error.message,
            });
          return errorResult;
        } finally {
          if (tabId) {
            set((state) => ({
              tabs: state.tabs.map((tab) =>
                tab.id === tabId ? { ...tab, isLoading: false } : tab
              ),
            }));
          }
        }
      },
      initializeApp: async () => {
        const { credential, setCredential } = get();
        if (credential.url && credential.username) {
          await setCredential(credential);
        }

        const db = await IndexedDB.initDB();
        set({ indexDbInstance: db });

        const tabs = await IndexedDB.getTabs(db);
        set({ tabs });

        if (tabs.length === 0) {
          await get().addTab({
            id: "home",
            title: "Home",
            content: "",
            type: "home",
          });
        }
        set({ isInitialized: true });
      },
      // Workspace State & Actions
      tabs: [],
      activeTab: "home",
      tabError: null,
      isTabLoading: false,
      indexDbInstance: null,

      addTab: async (tab) => {
        const { indexDbInstance, tabs } = get();
        if (!indexDbInstance) throw new Error("Database not initialized");

        const existingTab = tabs.find((t) => t.id === tab.id);
        if (existingTab) {
          set({ activeTab: existingTab.id });
          return;
        }
        await IndexedDB.addTab(indexDbInstance, tab);
        set((state) => ({
          tabs: [...state.tabs, tab],
          activeTab: tab.id,
        }));
      },
      updateTab: async (tabId, updates) => {
        const { indexDbInstance, tabs } = get();
        if (!indexDbInstance) throw new Error("Database not initialized");

        const updatedTabs = tabs.map((tab) =>
          tab.id === tabId ? { ...tab, ...updates } : tab
        );
        await IndexedDB.updateTab(
          indexDbInstance,
          updatedTabs.find((tab) => tab.id === tabId)!
        );
        set({ tabs: updatedTabs });
      },
      removeTab: async (tabId) => {
        const { indexDbInstance, tabs, activeTab } = get();
        if (!indexDbInstance) throw new Error("Database not initialized");

        await IndexedDB.removeTab(indexDbInstance, tabId);
        const updatedTabs = tabs.filter((tab) => tab.id !== tabId);
        set({ tabs: updatedTabs });

        if (activeTab === tabId) {
          set({ activeTab: updatedTabs[updatedTabs.length - 1]?.id || "home" });
        }
      },
      updateTabTitle: async (tabId, newTitle) => {
        const { indexDbInstance, tabs } = get();
        if (!indexDbInstance) throw new Error("Database not initialized");

        const updatedTabs = tabs.map((tab) =>
          tab.id === tabId ? { ...tab, title: newTitle } : tab
        );

        const updatedTab = updatedTabs.find((tab) => tab.id === tabId);
        if (!updatedTab) {
          throw new Error("Tab not found");
        }

        await IndexedDB.updateTab(indexDbInstance, updatedTab);
        set({ tabs: updatedTabs });
        toast.success(`Tab title updated to "${newTitle}"`);
      },
      setActiveTab: (tabId) => {
        set({ activeTab: tabId });
      },
      getTabById: (tabId) => {
        return get().tabs.find((tab) => tab.id === tabId);
      },
      moveTab: (oldIndex, newIndex) => {
        const tabs = [...get().tabs];
        const [removed] = tabs.splice(oldIndex, 1);
        tabs.splice(newIndex, 0, removed);
        set({ tabs });
      },
      // Explorer State & Actions
      dataBaseExplorer: [],
      isLoadingDatabase: false,
      isCreateTableModalOpen: false,
      isCreateDatabaseModalOpen: false,
      isUploadFileModalOpen: false,
      selectedDatabaseForCreateTable: "",
      selectedDatabaseForCreateDatabase: null,
      selectedTableForCreateTable: null,
      selectedTableForCreateDatabase: null,
      selectedDatabaseForDelete: null,
      selectedTableForDelete: null,
      selectedDatabaseForUpload: "",

      fetchDatabaseInfo: async () => {
        const { clickHouseClient } = get();
        if (!clickHouseClient) {
          throw new Error("ClickHouse client is not initialized");
        }
        set({ isLoadingDatabase: true });
        try {
          const query = appQueries.getDatabasesTables.query;
          if (!query) {
            throw new Error("getDatabasesTables query not found");
          }
          const result = await clickHouseClient.query({
            query,
          });
          const resultJSON = (await result.json()) as {
            data: Array<{
              database_name: string;
              table_name?: string;
              table_type?: string;
            }>;
          };
          const databases: Record<string, DatabaseInfo> = {};

          resultJSON.data.forEach((row) => {
            const { database_name, table_name, table_type } = row;
            if (!databases[database_name]) {
              databases[database_name] = {
                name: database_name,
                type: "database",
                children: [],
              };
            }
            if (table_name) {
              const table_type_mapped =
                table_type && table_type.toLowerCase() === "view"
                  ? "view"
                  : "table";
              databases[database_name].children.push({
                name: table_name,
                type: table_type_mapped,
              });
            }
          });

          const databasesArray = Object.values(databases).map((database) => ({
            ...database,
            children: database.children.length > 0 ? database.children : [],
          }));
          set({ dataBaseExplorer: databasesArray, isLoadingDatabase: false });
        } catch (error) {
          toast.error(
            `Failed to fetch database info: ${(error as Error).message}`
          );
          set({ isLoadingDatabase: false });
        }
      },

      closeCreateTableModal: () =>
        set({
          isCreateTableModalOpen: false,
          selectedDatabaseForCreateTable: "",
        }),
      openCreateTableModal: (database) =>
        set({
          isCreateTableModalOpen: true,
          selectedDatabaseForCreateTable: database,
        }),
      closeCreateDatabaseModal: () => set({ isCreateDatabaseModalOpen: false }),
      openCreateDatabaseModal: () => set({ isCreateDatabaseModalOpen: true }),
      closeUploadFileModal: () =>
        set({
          isUploadFileModalOpen: false,
          selectedDatabaseForUpload: "",
        }),
      openUploadFileModal: (database) =>
        set({
          isUploadFileModalOpen: true,
          selectedDatabaseForUpload: database,
        }),
      // Admin State & Actions
      isAdmin: false,
      savedQueries: {
        isSavedQueriesActive: false,
        isCheckingStatus: false,
        isActivating: false,
        isDeactivating: false,
        error: null,
      },
      checkIsAdmin: async (): Promise<boolean> => {
        const { clickHouseClient } = get();

        if (!clickHouseClient) {
          throw new ClickHouseError("ClickHouse client is not initialized");
        }

        try {
          const result = await clickHouseClient.query({
            query: `
                            SELECT if(grant_option = 1, true, false) AS is_admin 
                            FROM system.grants 
                            WHERE user_name = currentUser() 
                            LIMIT 1
                        `,
          });

          const response = (await result.json()) as AdminCheckResponse;

          if (!Array.isArray(response.data) || response.data.length === 0) {
            throw new ClickHouseError("No admin status data returned");
          }

          set({ isAdmin: response.data[0].is_admin });
          return response.data[0].is_admin;
        } catch (error) {
          const errorMessage =
            error instanceof Error ? error.message : "Unknown error occurred";
          console.error("Failed to check admin status:", errorMessage);
          set({ isAdmin: false });
          throw new ClickHouseError("Failed to check admin status", error);
        }
      },

      checkSavedQueriesStatus: async (): Promise<boolean> => {
        const { runQuery } = get();

        set((state) => ({
          savedQueries: {
            ...state.savedQueries,
            isCheckingStatus: true,
            error: null,
          },
        }));

        try {
          const result = await runQuery(`
                         SELECT COUNT(*) as exists 
                         FROM system.tables 
                         WHERE database = 'CH_UI' 
                         AND name = 'saved_queries'
                    `);
          const response = result as SavedQueriesCheckResponse;
          const isActive = response.data[0]?.exists > 0;

          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              isSavedQueriesActive: isActive,
            },
          }));

          return isActive;
        } catch (error) {
          const errorMessage = `Failed to check saved queries status: ${error}`;
          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              isSavedQueriesActive: false,
              error: errorMessage,
            },
          }));
          throw new ClickHouseError(errorMessage, error);
        } finally {
          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              isCheckingStatus: false,
            },
          }));
        }
      },

      activateSavedQueries: async () => {
        const { runQuery } = get();

        set((state) => ({
          savedQueries: {
            ...state.savedQueries,
            isActivating: true,
            error: null,
          },
        }));

        try {
          // Run queries in sequence with proper error handling
          await runQuery("CREATE DATABASE IF NOT EXISTS CH_UI").then(
            async () => {
              await runQuery(`
                            CREATE TABLE IF NOT EXISTS CH_UI.saved_queries (
                                id String,
                                name String,
                                query String,
                                created_at DateTime64(3),
                                updated_at DateTime64(3),
                                owner String,
                                is_public Boolean DEFAULT false,
                                tags Array(String) DEFAULT [],
                                description String DEFAULT '',
                                PRIMARY KEY (id)
                            ) ENGINE = MergeTree()
                            ORDER BY (id, created_at)
                            SETTINGS index_granularity = 8192
                        `);
            }
          );

          // Verify the table was created successfully
          const isActive = await get().checkSavedQueriesStatus();

          if (!isActive) {
            throw new ClickHouseError("Table creation verification failed");
          }

          toast.success("Saved queries activated successfully");
        } catch (error) {
          const errorMessage = "Failed to activate saved queries";
          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              error: errorMessage,
            },
          }));
          toast.error(errorMessage);
          throw new ClickHouseError(errorMessage, error);
        } finally {
          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              isActivating: false,
            },
          }));
        }
      },
      deactivateSavedQueries: async () => {
        const { runQuery } = get();

        set((state) => ({
          savedQueries: {
            ...state.savedQueries,
            isDeactivating: true,
            error: null,
          },
        }));

        try {
          await runQuery("DROP TABLE IF EXISTS CH_UI.saved_queries");

          // Verify the table was dropped successfully
          const isActive = await get().checkSavedQueriesStatus();

          if (isActive) {
            throw new ClickHouseError("Table deletion verification failed");
          }

          toast.success("Saved queries deactivated successfully");
          return true;
        } catch (error) {
          const errorMessage = "Failed to deactivate saved queries";
          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              error: errorMessage,
            },
          }));
          toast.error(errorMessage);
          throw new ClickHouseError(errorMessage, error);
        } finally {
          set((state) => ({
            savedQueries: {
              ...state.savedQueries,
              isDeactivating: false,
            },
          }));
        }
      },
    }),
    {
      name: "app-storage",
      partialize: (state) => ({
        credential: state.credential,
        activeTab: state.activeTab,
        clickhouseSettings: state.clickhouseSettings,
        isAdmin: state.isAdmin,
      }),
    }
  )
);

export default useAppStore;
