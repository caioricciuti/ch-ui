import { StateCreator } from 'zustand';
import { AppState, AdminSlice } from '@/types/common';
import { toast } from 'sonner';

// Define specific error types
export class ClickHouseError extends Error {
    constructor(message: string, public readonly originalError?: unknown) {
        super(message);
        this.name = 'ClickHouseError';
    }
}

// Define response types
interface AdminCheckResponse {
    data: Array<{ is_admin: boolean }>;
}

interface SavedQueriesCheckResponse {
    data: Array<{ exists: number }>;
}

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

    checkIsAdmin: async (): Promise<boolean> => {
        const { clickHouseClient } = get();

        if (!clickHouseClient) {
            throw new ClickHouseError('ClickHouse client is not initialized');
        }

        try {
            const result = await clickHouseClient.query({
                query: `
                    SELECT if(grant_option = 1, true, false) AS is_admin 
                    FROM system.grants 
                    WHERE user_name = currentUser() 
                    LIMIT 1
                `,
                format: 'JSON',
            });

            const response = (await result.json()) as AdminCheckResponse;

            if (!Array.isArray(response.data) || response.data.length === 0) {
                throw new ClickHouseError('No admin status data returned');
            }

            set({ isAdmin: response.data[0].is_admin });
            return response.data[0].is_admin;

        } catch (error) {
            const errorMessage = error instanceof Error ? error.message : 'Unknown error occurred';
            console.error('Failed to check admin status:', errorMessage);
            set({ isAdmin: false });
            throw new ClickHouseError('Failed to check admin status', error);
        }
    },

    checkSavedQueriesStatus: async (): Promise<boolean> => {
        const { runQuery } = get();

        set(state => ({
            savedQueries: {
                ...state.savedQueries,
                isCheckingStatus: true,
                error: null
            }
        }));

        try {
            const result = await runQuery(`
                 SELECT COUNT(*) as exists 
                 FROM system.tables 
                 WHERE database = 'CH_UI' 
                 AND name = 'saved_queries'
            `);

            const response = (result) as SavedQueriesCheckResponse;
            const isActive = response.data[0]?.exists > 0;

            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isSavedQueriesActive: isActive
                }
            }));

            return isActive;

        } catch (error) {
            const errorMessage = `Failed to check saved queries status: ${error}`;
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    isSavedQueriesActive: false,
                    error: errorMessage
                }
            }));
            throw new ClickHouseError(errorMessage, error);
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
            // Run queries in sequence with proper error handling
            await runQuery('CREATE DATABASE IF NOT EXISTS CH_UI').then(async () => {
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
            });

            // Verify the table was created successfully
            const isActive = await get().checkSavedQueriesStatus();

            if (!isActive) {
                throw new ClickHouseError('Table creation verification failed');
            }

            toast.success('Saved queries activated successfully');

        } catch (error) {
            const errorMessage = 'Failed to activate saved queries';
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    error: errorMessage
                }
            }));
            toast.error(errorMessage);
            throw new ClickHouseError(errorMessage, error);
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
            await runQuery('DROP TABLE IF EXISTS CH_UI.saved_queries');

            // Verify the table was dropped successfully
            const isActive = await get().checkSavedQueriesStatus();

            if (isActive) {
                throw new ClickHouseError('Table deletion verification failed');
            }

            toast.success('Saved queries deactivated successfully');
            return true;

        } catch (error) {
            const errorMessage = 'Failed to deactivate saved queries';
            set(state => ({
                savedQueries: {
                    ...state.savedQueries,
                    error: errorMessage
                }
            }));
            toast.error(errorMessage);
            throw new ClickHouseError(errorMessage, error);
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