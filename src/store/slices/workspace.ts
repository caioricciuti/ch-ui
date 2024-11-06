// src/store/slices/workspace.ts
import { StateCreator } from 'zustand';
import { AppState, WorkspaceSlice } from '@/types/common';
import * as IndexedDB from '@/lib/indexDB';
import { toast } from 'sonner';

export const createWorkspaceSlice: StateCreator<
    AppState,
    [],
    [],
    WorkspaceSlice
> = (set, get) => ({
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
});