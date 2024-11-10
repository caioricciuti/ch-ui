// src/store/slices/explorer.ts
import { StateCreator } from 'zustand';
import { AppState, ExplorerSlice } from '@/types/common';
import { appQueries } from '@/features/workspace/editor/appQueries';
import { toast } from 'sonner';

interface DatabaseInfo {
    name: string;
    type: "database";
    children: Array<{ name: string; type: "table" | "view" }>;
}

export const createExplorerSlice: StateCreator<
    AppState,
    [],
    [],
    ExplorerSlice
> = (set, get) => ({
    dataBaseExplorer: [],
    isLoadingDatabase: false,
    isCreateTableModalOpen: false,
    isCreateDatabaseModalOpen: false,
    selectedDatabaseForCreateTable: "",
    selectedDatabaseForCreateDatabase: null,
    selectedTableForCreateTable: null,
    selectedTableForCreateDatabase: null,
    selectedDatabaseForDelete: null,
    selectedTableForDelete: null,

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
                query
            });
            const resultJSON = await result.json() as { data: Array<{
                database_name: string;
                table_name?: string;
                table_type?: string;
            }>};
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
            toast.error(`Failed to fetch database info: ${(error as Error).message}`);
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

    closeCreateDatabaseModal: () =>
        set({ isCreateDatabaseModalOpen: false }),

    openCreateDatabaseModal: () =>
        set({ isCreateDatabaseModalOpen: true }),
});