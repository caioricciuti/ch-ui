// src/store/store.ts
import { create } from "zustand";
import { persist } from "zustand/middleware";
import {
  AppState,
  Credential,
  DatabaseInfo,
  ClickHouseSettings,
  QueryResult,
  SavedQuery,
} from "@/types/common";
import { createClient } from "@clickhouse/client-web";
import { isCreateOrInsert } from "@/helpers/sqlUtils";
import * as IndexedDB from "@/lib/indexDB";
import { OverflowMode } from "@clickhouse/client-common/dist/settings";
import { toast } from "sonner";
import { appQueries } from "@/features/workspace/editor/appQueries";

/**
 * Error class for ClickHouse related errors.
 */
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

/**
 * Helper: Builds the connection URL from the provided credential.
 */
const buildConnectionUrl = (credential: Credential): string => {
  let baseUrl = credential.url.replace(/\/+$/, "");
  if (credential.useAdvanced && credential.customPath) {
    const cleanPath = credential.customPath.replace(/^\/+/, "");
    return `${baseUrl}/${cleanPath}`;
  }
  return baseUrl;
};

const escapeClickhouseString = (value: string): string =>
  value
    .replace(/\\/g, "\\\\") // Escape backslashes
    .replace(/'/g, "''") // Escape single quotes
    .replace(/\n/g, "\\n") // Escape newline characters
    .replace(/\r/g, "\\r"); // Escape carriage returns

/**
 * Zustand Store
 */
const useAppStore = create<AppState>()(
  persist(
    (set, get) => {
      return {
        // =====================================================
        // Credentials & Connection State and Actions
        // =====================================================
        credential: {
          url: "",
          username: "",
          password: "",
          useAdvanced: false,
          customPath: "",
          requestTimeout: 30000,
        },
        clickHouseClient: null,
        isLoadingCredentials: false,
        isServerAvailable: false,
        isInitialized: false,
        version: "",
        error: "",
        credentialSource: null,
        updatedSavedQueriesTrigger: "",
        clickhouseSettings: {
          max_result_rows: "0",
          max_result_bytes: "0",
          result_overflow_mode: "break" as OverflowMode,
        },

        /**
         * Sets the credential source.
         */
        setCredentialSource: (source) => set({ credentialSource: source }),

        /**
         * Sets credentials, initializes the ClickHouse client, and checks the server status.
         */
        setCredential: async (credential: Credential) => {
          set({ credential, isLoadingCredentials: true });
          try {
            const connectionUrl = buildConnectionUrl(credential);
            const client = createClient({
              url: connectionUrl,
              pathname: credential.customPath, // Use custom path for proxy
              username: credential.username,
              password: credential.password || "",
              request_timeout: credential.requestTimeout || 30000,
              clickhouse_settings: {
                ...get().clickhouseSettings,
                result_overflow_mode: "break",
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
            toast.error(
              `Failed to set credentials: ${(error as Error).message}`
            );
          } finally {
            set({ isLoadingCredentials: false });
          }
        },

        /**
         * Updates ClickHouse configuration and re-checks the server status.
         */
        updateConfiguration: async (clickhouseSettings: ClickHouseSettings) => {
          try {
            const credentials = get().credential;
            const connectionUrl = buildConnectionUrl(credentials);
            const client = createClient({
              url: connectionUrl,
              pathname: credentials.customPath, // Ensure custom path is applied
              username: credentials.username,
              password: credentials.password || "",
              request_timeout: credentials.requestTimeout || 30000,
              clickhouse_settings: clickhouseSettings,
            });
            set({ clickHouseClient: client, clickhouseSettings });
            await get().checkServerStatus();
          } catch (error) {
            throw error;
          }
        },

        /**
         * Clears all stored credentials and resets connection settings.
         */
        clearCredentials: async () => {
          set({
            credential: {
              url: "",
              username: "",
              password: "",
              useAdvanced: false,
              customPath: "",
              requestTimeout: 30000,
            },
            clickhouseSettings: {
              max_result_rows: "0",
              max_result_bytes: "0",
              result_overflow_mode: "break" as OverflowMode,
            },
            clickHouseClient: null,
            isServerAvailable: false,
            version: "",
            error: "",
          });
        },

        /**
         * Pings the ClickHouse server and retrieves its version.
         * If the connection fails, credentials are cleared.
         */
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

        /**
         * Runs a SQL query. If a tabId is provided, it updates the tab state.
         * Supports both query and command operations.
         */
        runQuery: async (query: string, tabId?: string) => {
          const { clickHouseClient } = get();
          if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
          }
          if (tabId) {
            set((state) => ({
              tabs: state.tabs.map((tab) =>
                tab.id === tabId
                  ? { ...tab, isLoading: true, error: null }
                  : tab
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
                statistics: { elapsed: 0, rows_read: 0, bytes_read: 0 },
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

            const result = await clickHouseClient.query({
              query: trimmedQuery,
            });
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
              statistics: { elapsed: 0, rows_read: 0, bytes_read: 0 },
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

        /**
         * Initializes the app by setting credentials (if available), initializing IndexedDB,
         * fetching tabs, and ensuring a home tab exists.
         */
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

        // =====================================================
        // Workspace / Tabs State and Actions
        // =====================================================

        tabs: [],
        activeTab: "home",
        tabError: null,
        isTabLoading: false,
        indexDbInstance: null,

        /**
         * Adds a new tab. If the tab already exists, it simply activates it.
         */
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

        /**
         * Updates a tab with the given changes.
         */
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

        /**
         * Removes a tab and updates the active tab if necessary.
         */
        removeTab: async (tabId) => {
          const { indexDbInstance, tabs, activeTab } = get();
          if (!indexDbInstance) throw new Error("Database not initialized");
          await IndexedDB.removeTab(indexDbInstance, tabId);
          const updatedTabs = tabs.filter((tab) => tab.id !== tabId);
          set({ tabs: updatedTabs });
          if (activeTab === tabId) {
            set({
              activeTab: updatedTabs[updatedTabs.length - 1]?.id || "home",
            });
          }
        },

        /**
         * Creates a duplicate of an existing tab.
         */
        duplicateTab: async (tabId: string) => {
          const { indexDbInstance, tabs } = get();
          if (!indexDbInstance) throw new Error("Database not initialized");
          const tabToDuplicate = tabs.find((tab) => tab.id === tabId);
          if (!tabToDuplicate) {
            throw new Error("Tab not found");
          }
          const newTab = {
            ...tabToDuplicate,
            id: `tab-${Date.now()}`,
            title: `${tabToDuplicate.title} (Copy)`,
          };
          await IndexedDB.addTab(indexDbInstance, newTab);
          set((state) => ({
            tabs: [...state.tabs, newTab],
            activeTab: newTab.id,
          }));
        },

        /**
         * Closes all tabs except the home tab.
         */
        closeAllTabs: async () => {
          const { indexDbInstance, tabs } = get();
          if (!indexDbInstance) throw new Error("Database not initialized");
          const tabsToRemove = tabs.filter((tab) => tab.id !== "home");
          await Promise.all(
            tabsToRemove.map((tab) =>
              IndexedDB.removeTab(indexDbInstance, tab.id)
            )
          );
          set({
            tabs: [tabs.find((tab) => tab.id === "home")!],
            activeTab: "home",
          });
        },

        /**
         * Updates the title of a specified tab.
         */
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

        /**
         * Sets the active tab.
         */
        setActiveTab: (tabId) => {
          set({ activeTab: tabId });
        },

        /**
         * Retrieves a tab by its ID.
         */
        getTabById: (tabId) => {
          return get().tabs.find((tab) => tab.id === tabId);
        },

        /**
         * Moves a tab from one position to another.
         */
        moveTab: (oldIndex, newIndex) => {
          const tabs = [...get().tabs];
          const [removed] = tabs.splice(oldIndex, 1);
          tabs.splice(newIndex, 0, removed);
          set({ tabs });
        },

        // =====================================================
        // Explorer State and Actions
        // =====================================================
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

        /**
         * Fetches database and table information from ClickHouse
         * and organizes it for the explorer.
         */
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
            const result = await clickHouseClient.query({ query });
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

        // Modal controls for explorer
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
        closeCreateDatabaseModal: () =>
          set({ isCreateDatabaseModalOpen: false }),
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

        // =====================================================
        // Admin & Saved Queries Actions
        // =====================================================
        isAdmin: false,
        savedQueries: {
          isSavedQueriesActive: false,
          isCheckingStatus: false,
          isActivating: false,
          isDeactivating: false,
          error: null,
        },

        /**
         * Checks if the current user has admin privileges.
         */

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

        /**
         * Checks if the saved queries table exists in ClickHouse.
         */
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

        /**
         * Activates saved queries by creating the necessary database and table.
         */
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

        /**
         * Deactivates saved queries by dropping the saved queries table.
         */
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

        saveQuery: async (
          tabId: string,
          name: string,
          query: string,
          isPublic: boolean = false
        ) => {
          const { clickHouseClient, updateTab } = get(); // Removed fetchSavedQueries since we'll use the trigger
          if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
          }

          try {
            const safeName = escapeClickhouseString(name);
            const safeQuery = escapeClickhouseString(query);

            const insertQuery = `
              INSERT INTO CH_UI.saved_queries (id, name, query, created_at, updated_at, owner, is_public)
              VALUES (
                '${tabId}',
                '${safeName}',
                '${safeQuery}',
                now(),
                now(),
                currentUser(),
                ${isPublic}
              )
            `;

            await clickHouseClient.command({ query: insertQuery });

            await updateTab(tabId, {
              type: "saved_query",
              title: name,
              content: query,
              isSaved: true,
            });

            // Update the trigger to refresh saved queries
            set((state) => ({
              updatedSavedQueriesTrigger: Date.now().toString(),
            }));

            toast.success(`Query "${name}" saved successfully!`);
          } catch (error: any) {
            console.error("Failed to save query:", error);
            toast.error(`Failed to save query: ${error.message}`);
            throw new ClickHouseError("Failed to save query", error);
          }
        },

        updateSavedQuery: async (tabId: string, query: string) => {
          const { clickHouseClient, updateTab, getTabById, runQuery } = get();
          if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
          }
          try {
            const tab = getTabById(tabId);
            if (!tab || !tab.title) {
              throw new Error(
                "Tab is not a saved query or queryName is missing"
              );
            }
            const name = tab.title;

            const safeName = escapeClickhouseString(name);
            const safeQuery = escapeClickhouseString(query);

            const updateQuery = `
              ALTER TABLE CH_UI.saved_queries
              UPDATE
                name = '${safeName}',
                query = '${safeQuery}',
                updated_at = now()
              WHERE id = '${tabId}'
            `;

            await runQuery(updateQuery);

            await updateTab(tabId, {
              title: tab.title,
              content: query,
            });

            // Increment the trigger to force a refresh
            set((state) => ({
              updatedSavedQueriesTrigger: Date.now().toString(),
            }));

            toast.success(`Query "${tab.title}" updated successfully!`);
          } catch (error: any) {
            console.error("Failed to update query:", error);
            toast.error(`Failed to update query: ${error.message}`);
            throw new ClickHouseError("Failed to update query", error);
          }
        },

        deleteSavedQuery: async (id: string) => {
          const { clickHouseClient, removeTab } = get();
          if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
          }
          try {
            await clickHouseClient.command({
              query: `ALTER TABLE CH_UI.saved_queries DELETE WHERE id = '${id}'`,
            });
            await removeTab(id);

            // Increment the trigger to force a refresh
            set((state) => ({
              updatedSavedQueriesTrigger: Date.now().toString(),
            }));

            toast.success("Query deleted successfully!");
          } catch (error: any) {
            console.error("Failed to delete query:", error);
            toast.error(`Failed to delete query: ${error.message}`);
            throw new ClickHouseError("Failed to delete query", error);
          }
        },

        fetchSavedQueries: async (id?) => {
          const { clickHouseClient } = get();
          if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
          }
          try {
            let query;
            if (id) {
              query = `SELECT * FROM CH_UI.saved_queries WHERE id = '${id}'`;
            } else {
              query = appQueries.getSavedQueries.query;
            }
            const result = await clickHouseClient.query({ query });
            const jsonResult = (await result.json()) as { data: SavedQuery[] };
            return jsonResult.data;
          } catch (error: any) {
            console.error("Failed to fetch saved queries:", error);
            toast.error(`Failed to fetch saved queries: ${error.message}`);
            return [];
          }
        },
      };
    },

    {
      name: "app-storage",
      // Persist only a subset of the state
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
