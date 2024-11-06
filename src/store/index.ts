// src/store/index.ts
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { AppState } from '@/types/common';
import { createCoreSlice } from './slices/core';
import { createWorkspaceSlice } from "@/store/slices/workspace"
import { createAdminSlice } from '@/store/slices/admin';
import { createExplorerSlice } from '@/store/slices/explorer';

const useAppStore = create<AppState>()(
    persist(
        (...a) => ({
            ...createCoreSlice(...a),
            ...createWorkspaceSlice(...a),
            ...createAdminSlice(...a),
            ...createExplorerSlice(...a),
        }),
        {
            name: 'app-storage',
            partialize: (state) => ({
                credential: state.credential,
                activeTab: state.activeTab,
                clickhouseSettings: state.clickhouseSettings
            }),
        }
    )
);

// Optional: Create specific hooks for better code organization
export const useWorkspace = () => {
    const store = useAppStore();
    return {
        tabs: store.tabs,
        activeTab: store.activeTab,
        addTab: store.addTab,
        updateTab: store.updateTab,
        removeTab: store.removeTab,
        setActiveTab: store.setActiveTab,
        getTabById: store.getTabById,
        moveTab: store.moveTab,
    };
};

export const useAdmin = () => {
    const store = useAppStore();
    return {
        isAdmin: store.isAdmin,
        savedQueries: store.savedQueries,
        checkIsAdmin: store.checkIsAdmin,
        activateSavedQueries: store.activateSavedQueries,
        deactivateSavedQueries: store.deactivateSavedQueries,
        checkSavedQueriesStatus: store.checkSavedQueriesStatus,
    };
};

export const useExplorer = () => {
    const store = useAppStore();
    return {
        dataBaseExplorer: store.dataBaseExplorer,
        isLoadingDatabase: store.isLoadingDatabase,
        fetchDatabaseInfo: store.fetchDatabaseInfo,
        openCreateTableModal: store.openCreateTableModal,
        closeCreateTableModal: store.closeCreateTableModal,
        openCreateDatabaseModal: store.openCreateDatabaseModal,
        closeCreateDatabaseModal: store.closeCreateDatabaseModal,
    };
};

export default useAppStore;