// src/store/slices/core.ts
import { StateCreator } from 'zustand';
import { AppState, CoreSlice } from '@/types/common';
import { ClickHouseSettings, createClient } from "@clickhouse/client-web";
import { toast } from 'sonner';
import { isCreateOrInsert } from '@/helpers/sqlUtils';
import * as IndexedDB from '@/lib/indexDB';
import { OverflowMode } from "@clickhouse/client-common/dist/settings"

export const createCoreSlice: StateCreator<
    AppState,
    [],
    [],
    CoreSlice
> = (set, get) => ({
    credential: {} as Credential,
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
        result_overflow_mode: "break" as OverflowMode
    },

    setCredentialSource: (source) => set({ credentialSource: source }),

    setCredential: async (credential) => {
        set({ credential, isLoadingCredentials: true });
        try {
            const client = createClient({
                url: credential.host,
                username: credential.username,
                password: credential.password || "",
                clickhouse_settings: get().clickhouseSettings
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

    updateConfiguration: async (clickhouseSettings: ClickHouseSettings) => {
        try {
            const credentials = get().credential
            const client = createClient({
                url: credentials.host,
                username: credentials.username,
                password: credentials.password || "",
                clickhouse_settings: clickhouseSettings
            });
            set({ clickHouseClient: client, clickhouseSettings });
            await get().checkServerStatus();
        } catch (error) {
            set({ error: (error as Error).message });
            toast.error(`Failed to update configuration: ${(error as Error).message}`);
        }
    },

    clearCredentials: async () => {
        set({
            credential: {} as Credential,
            clickhouseSettings: {
                max_result_rows: "0",
                max_result_bytes: "0",
                result_overflow_mode: "break"
            } ,
            clickHouseClient: null,
            isServerAvailable: false,
            version: "",
            error: ""
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
            const versionData = await versionResult.json() as { data: { "version()": string }[] };
            const version = versionData.data[0]["version()"];
            set({ isServerAvailable: true, version });
        } catch (error: any) {
            set({ isServerAvailable: false, error: error.message });
        } finally {
            set({ isLoadingCredentials: false });
        }
    },

    runQuery: async (query, tabId) => {
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
            let result;
            if (isCreateOrInsert(query)) {
                result = await clickHouseClient.command({
                    query: query,
                });
                return { message: "Query executed successfully" };
            } else {
                result = await clickHouseClient.query({
                    query,
                    format: "JSON",
                });
                const jsonResult = await result.json();

                const processedResult = {
                    meta: jsonResult.meta || [],
                    data: jsonResult.data || [],
                    statistics: jsonResult.statistics || {
                        elapsed: 0,
                        rows_read: 0,
                        bytes_read: 0,
                    },
                    message: jsonResult.data?.length === 0
                        ? "No data returned from query"
                        : null,
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
            }
        } catch (error: any) {
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
});