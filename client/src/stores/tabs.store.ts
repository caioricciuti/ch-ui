import { create } from "zustand";
import { persist } from "zustand/middleware";
import { v4 as uuidv4 } from "uuid";
import api from "../api/axios.config";
import { TabQueryState, Tab } from "@/types/types";

const useTabStore = create<TabQueryState>()(
  persist(
    (set, get) => ({
      tabs: [
        {
          id: "home",
          title: "Home",
          content: "",
          type: "home",
          error: null,
        },
      ],
      activeTabId: "home",
      isLoading: false,
      error: null,

      addTab: (tab) => {
        const newTab = { ...tab, id: uuidv4() };
        set((state) => ({
          tabs: [...state.tabs, newTab],
          activeTabId: newTab.id,
        }));
      },

      closeTab: (id) => {
        set((state) => {
          const tabIndex = state.tabs.findIndex((tab) => tab.id === id);
          if (tabIndex === -1) return state;

          const newTabs = state.tabs.filter((tab) => tab.id !== id);
          let newActiveTabId = state.activeTabId;

          if (id === state.activeTabId) {
            newActiveTabId =
              newTabs[tabIndex]?.id ||
              newTabs[tabIndex - 1]?.id ||
              newTabs[0]?.id ||
              "";
          }

          return { tabs: newTabs, activeTabId: newActiveTabId };
        });
      },

      updateTabContent: (id: string, updatedValues: Partial<Tab>) => {
        set((state) => ({
          tabs: state.tabs.map((tab) =>
            tab.id === id ? { ...tab, ...updatedValues } : tab
          ),
        }));
      },

      setActiveTab: (id) => {
        set({ activeTabId: id });
      },

      updateTabTitle: (id, title) => {
        set((state) => ({
          tabs: state.tabs.map((tab) =>
            tab.id === id ? { ...tab, title } : tab
          ),
        }));
      },

      moveTab: (fromIndex, toIndex) => {
        set((state) => {
          const newTabs = [...state.tabs];
          const [movedTab] = newTabs.splice(fromIndex, 1);
          newTabs.splice(toIndex, 0, movedTab);
          return { tabs: newTabs };
        });
      },

      getTabById: (id) => {
        return get().tabs.find((tab) => tab.id === id);
      },

      fetchQueries: async () => {
        set({ isLoading: true, error: null });
        try {
          const response = await api.get("/ch-queries");
          console.log("Fetched queries:", response.data);
        } catch (err) {
          set({ error: "Failed to fetch queries. Please try again later." });
          console.error("Error fetching queries:", err);
        } finally {
          set({ isLoading: false });
        }
      },

      runQuery: async (tabId, query) => {
        set((state) => ({
          tabs: state.tabs.map((tab) =>
            tab.id === tabId ? { ...tab, isLoading: true, error: null } : tab
          ),
        }));

        try {
          const response = await api.post("/query", { query });
          set((state) => ({
            tabs: state.tabs.map((tab) =>
              tab.id === tabId
                ? { ...tab, results: response.data, isLoading: false }
                : tab
            ),
          }));
        } catch (err: any) {
          console.log(err)
          set((state) => ({
            tabs: state.tabs.map((tab) =>
              tab.id === tabId
                ? {
                    ...tab,
                    error: err.data,
                    isLoading: false,
                  }
                : tab
            ),
          }));
        }
      },
    }),
    {
      name: "tab-query-storage",
      partialize: (state) => ({
        tabs: state.tabs.map(({ id, title, content, type }) => ({
          id,
          title,
          content,
          type,
        })),
        activeTabId: state.activeTabId,
      }),
    }
  )
);

export default useTabStore;
