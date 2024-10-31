// src/store/slices/admin.ts
import { StateCreator } from 'zustand';
import { AppState, AdminSlice } from '@/types/common';
import { toast } from 'sonner';

export const createAdminSlice: StateCreator<
    AppState,
    [],
    [],
    AdminSlice
> = (set, get) => ({
    isAdmin: false,
    savedQueries: {
        isSavedQueriesActive: false,
        isCheckingStatus: false,
        isActivating: false,
        isDeactivating: false,
        error: null
    },

    checkIsAdmin: async () => {
        const { clickHouseClient } = get();
        if (!clickHouseClient) {
            throw new Error("ClickHouse client is not initialized");
        }

        try {
            const result = await clickHouseClient.query({
                query: `SELECT if(grant_option = 1, true, false) AS is_admin 
                FROM system.grants 
                WHERE user_name = currentUser() 
                LIMIT 1`,
                format: "JSON",
            });
            const resultJSON = await result.json();

            if (!Array.isArray(resultJSON.data) || resultJSON.data.length === 0) {
                throw new Error("Invalid data format");
            }
            set({ isAdmin: resultJSON?.data[0].is_admin as boolean });
        } catch (error) {
            console.error("Failed to check admin status:", error);
            set({ isAdmin: false });
        }
    },

    checkSavedQueriesStatus: async () => {
        const { runQuery } = get();
        set(state => ({
            savedQueries: {
                ...state.savedQueries,
                isCheckingStatus: true,
                error: null
            }
        }));

        try {
            const result = await runQuery("DESCRIBE CH_UI.saved_queries");
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isSavedQueriesActive: result.data.length > 0,
                }
            }));
        } catch (error) {
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isSavedQueriesActive: false,
                    error: "Failed to check saved queries status"
                }
            }));
        } finally {
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isCheckingStatus: false
                }
            }));
        }
    },

    activateSavedQueries: async () => {
        const { runQuery } = get();
        set(state => ({
            savedQueries: {
                ...state.savedQueries,
                isActivating: true,
                error: null
            }
        }));

        try {
            await runQuery("CREATE DATABASE IF NOT EXISTS CH_UI");
            await runQuery(`
        CREATE TABLE IF NOT EXISTS CH_UI.saved_queries (
          id String,
          name String,
          query String,
          created_at DateTime,
          updated_at DateTime,
          owner String,
          PRIMARY KEY (id)
        ) ENGINE = MergeTree()
      `);

            await get().checkSavedQueriesStatus();
            toast.success("Saved queries activated successfully");
        } catch (error) {
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    error: "Failed to activate saved queries"
                }
            }));
            toast.error("Failed to activate saved queries");
        } finally {
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isActivating: false
                }
            }));
        }
    },

    deactivateSavedQueries: async () => {
        const { runQuery } = get();
        set(state => ({
            savedQueries: {
                ...state.savedQueries,
                isDeactivating: true,
                error: null
            }
        }));

        try {
            await runQuery("DROP TABLE IF EXISTS CH_UI.saved_queries");
            await get().checkSavedQueriesStatus();
            toast.success("Saved queries deactivated successfully");
        } catch (error) {
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    error: "Failed to deactivate saved queries"
                }
            }));
            toast.error("Failed to deactivate saved queries");
        } finally {
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isDeactivating: false
                }
            }));
        }
    },
});