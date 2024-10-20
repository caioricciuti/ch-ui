import { create } from "zustand";
import { persist } from "zustand/middleware";
import { ClickHouseClient, createClient, ResponseJSON } from "@clickhouse/client-web";
import { Credential, Tab, DatabaseInfo, AppState } from "@/types";
import { toast } from "sonner";
import { appQueries } from "@/helpers/appQueries";
import * as IndexedDB from "@/lib/indexDB";
import { isCreateOrInsert } from "@/helpers/sqlUtils";

const useAppStore = create<AppState>()(
  persist(
    (set, get) => ({
      credential: {} as Credential,
      tabs: [],
      activeTab: "home",
      isLoadingCredentials: false,
      isServerAvailable: false,
      isInitialized: false,
      version: "",
      error: "",
      clickHouseClient: null,
      dataBaseExplorer: [],
      isLoadingDatabase: false,
      tabError: null,
      isTabLoading: false,
      isCreateTableModalOpen: false,
      isCreateDatabaseModalOpen: false,
      selectedDatabaseForCreateTable: "",
      selectedDatabaseForCreateDatabase: null,
      selectedTableForCreateTable: null,
      selectedTableForCreateDatabase: null,
      selectedDatabaseForDelete: null,
      selectedTableForDelete: null,
      indexDbInstance: null,
      credentialSource: null,

      setCredentialSource: (source: "env" | "app") => {
        set({ credentialSource: source });
      },

      setCredential: async (credential) => {
        set({ credential, isLoadingCredentials: true });
        try {
          const client: ClickHouseClient = createClient({
            url: credential.host,
            username: credential.username,
            password: credential.password,
          });
          set({ clickHouseClient: client });
          await get().checkServerStatus();
        } catch (error) {
          set({ error: (error as Error).message });
          toast.error(`Failed to set credentials: ${(error as Error).message}`);
        } finally {
          set({ isLoadingCredentials: false });
        }
      },

      clearCredentials: async () => {
        set({
          credential: {} as Credential,
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
            format: "JSON",
          });
          const versionData = await versionResult.json();
          const version = (versionData as { data: { "version()": string }[] })
            .data[0]["version()"];
          set({ isServerAvailable: true, version });
        } catch (error: any) {
          set({ isServerAvailable: false, error: error.message });
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

        let jsonResult: ResponseJSON<"JSON"> = {
          data: [],
          meta: [],
          statistics: { elapsed: 0, rows_read: 0, bytes_read: 0 },
          rows: 0,
        };
        
        try {
          let result;
          if (isCreateOrInsert(query)) {
            result = await clickHouseClient.command({
              query: query,
            });  
          } else {
            result = await clickHouseClient.query({
              query,
              format: "JSON",
            });
            jsonResult = await result.json();
          }

          

          const processedResult = {
            meta: jsonResult.meta || [],
            data: jsonResult.data || [],
            statistics: jsonResult.statistics || {
              elapsed: 0,
              rows_read: 0,
              bytes_read: 0,
            },
            message:
              jsonResult.data && jsonResult.data.length > 0 && jsonResult.meta && jsonResult.meta.length > 0
                ? null
                : "Query executed successfully",
            rows: jsonResult.rows || 0,
          };

          if (tabId) {
            await get().updateTab(tabId, {
              result: processedResult,
              isLoading: false,
              error: null,
            });
          }
          return processedResult;
        } catch (error: any) {
          console.error("Error running query:", error);
          const errorResult = {
            meta: [],
            data: [],
            statistics: { elapsed: 0, rows_read: 0, bytes_read: 0 },
            message: null,
            error: error.message || "An error occurred while running the query",
            rows: 0,
          };

          if (tabId) {
            await get().updateTab(tabId, {
              result: errorResult,
              isLoading: false,
              error: errorResult.error,
            });
          }
          return errorResult;
        }
      },

      initializeApp: async () => {
        const { credential, setCredential } = get();
        if (credential.host && credential.username) {
          await setCredential(credential);
        }

        // Initialize IndexedDB
        const db = await IndexedDB.initDB();
        set({ indexDbInstance: db });

        // Load tabs from IndexedDB
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

      fetchDatabaseInfo: async () => {
        const { clickHouseClient } = get();
        if (!clickHouseClient) {
          throw new Error("ClickHouse client is not initialized");
        }
        set({ isLoadingDatabase: true, tabError: null });
        try {
          const query = appQueries.getDatabasesTables.query;
          if (!query) {
            throw new Error("getDatabasesTables query not found");
          }
          const result = await clickHouseClient.query({
            query,
            format: "JSON",
          });
          const resultJSON = await result.json();
          const databases: Record<string, DatabaseInfo> = {};
          resultJSON.data.forEach((row: any) => {
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
          set({ tabError: (error as Error).message, isLoadingDatabase: false });
          toast.error(
            `Failed to fetch database info: ${(error as Error).message}`
          );
        }
      },

      addTab: async (tab: Tab) => {
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

      updateTab: async (tabId: string, updates: Partial<Tab>) => {
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

      updateTabTitle: async (tabId: string, newTitle: string) => {
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

      removeTab: async (tabId: string) => {
        const { indexDbInstance, tabs, activeTab } = get();
        if (!indexDbInstance) throw new Error("Database not initialized");

        await IndexedDB.removeTab(indexDbInstance, tabId);
        const updatedTabs = tabs.filter((tab) => tab.id !== tabId);
        set({ tabs: updatedTabs });

        if (activeTab === tabId) {
          set({ activeTab: updatedTabs[updatedTabs.length - 1]?.id || "home" });
        }
      },

      setActiveTab: (tabId: string) => {
        set({ activeTab: tabId });
      },

      getTabById: (tabId: string) => {
        return get().tabs.find((tab) => tab.id === tabId);
      },

      moveTab: (oldIndex: number, newIndex: number) => {
        const tabs = [...get().tabs];
        const [removed] = tabs.splice(oldIndex, 1);
        tabs.splice(newIndex, 0, removed);
        set({ tabs });
      },

      closeCreateTableModal: () =>
        set({
          isCreateTableModalOpen: false,
          selectedDatabaseForCreateTable: "",
        }),

      openCreateTableModal: (database: string) =>
        set({
          isCreateTableModalOpen: true,
          selectedDatabaseForCreateTable: database,
        }),

      closeCreateDatabaseModal: () => set({ isCreateDatabaseModalOpen: false }),

      openCreateDatabaseModal: () => set({ isCreateDatabaseModalOpen: true }),
    }),

    {
      name: "app-storage",
      partialize: (state) => ({
        credential: state.credential,
        activeTab: state.activeTab,
        // We don't persist tabs here as they're handled by IndexedDB
      }),
    }
  )
);

export default useAppStore;
