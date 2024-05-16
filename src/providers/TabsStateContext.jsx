import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
} from "react";
import { toast } from "sonner";
import { useClickHouseState } from "@/providers/ClickHouseContext";
import { readFromDB, writeToDB, deleteFromDB } from "@/lib/tab.index.db";

const TabStateContext = createContext();

export const TabsStateProvider = ({ children }) => {
  const { clickHouseClient } = useClickHouseState();
  const [tabs, setTabs] = useState([]);
  const [activeTab, setActiveTab] = useState(
    localStorage.getItem("activeTab") || "home"
  );
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingQuery, setIsLoadingQuery] = useState(false);

  useEffect(() => {
    const fetchData = async () => {
      await fetchTabsFromDb();
    };

    fetchData();
  }, []);

  useEffect(() => {
    if (!tabs.length) return;
    localStorage.setItem("activeTab", activeTab);
    if (!tabs.find((tab) => tab.tab_id === activeTab)) {
      setActiveTab("home");
    }
  }, [activeTab, tabs]);

  const fetchTabsFromDb = async () => {
    try {
      setIsLoading(true); // Ensure loading state is set to true when operation starts
      let tabsData = await readFromDB("tabs");
      tabsData.sort((a, b) => a.order - b.order);
      // Check if the "home" tab is present
      const hasHomeTab = tabsData.find((tab) => tab.tab_id === "home");
      if (tabsData.length === 1) {
        setActiveTab("home");
      }
      if (!hasHomeTab) {
        // Define the "home" tab
        const homeTab = {
          tab_id: "home",
          tab_title: "Home",
          tab_type: "home",
          order: 0,
        };
        // Add the "home" tab to the database
        await writeToDB(homeTab);
        // Prepend the "home" tab to the tabs array
        tabsData.unshift(homeTab); // This puts "home" at the first position
      }

      // Always ensure the home tab is at the first position if it exists
      if (hasHomeTab && tabsData[0].tab_id !== "home") {
        // Remove the home tab from its current position
        tabsData = tabsData.filter((tab) => tab.tab_id !== "home");
        // Prepend the home tab
        tabsData.unshift(hasHomeTab);
      }
      setTabs(tabsData);
      setIsLoading(false); // Reset loading state once operation is complete
    } catch (error) {
      toast.error("Error fetching tabs: " + error.message);
    }
  };

  const addQueryTab = useCallback(
    async (tab_config) => {
      const newTab = {
        tab_id: Math.random().toString(36).substring(2, 15),
        tab_title: tab_config?.tab_title || "New Tab",
        tab_type: "query",
        tab_content: tab_config?.tab_content || "",
        last_run: null,
        tab_errors: null,
        tab_results: null,
        order: tabs.length,
      };
      setTabs((prevTabs) => [...prevTabs, newTab]);
      setActiveTab(newTab.tab_id);
    },
    [tabs]
  );

  const updateQueryTab = (tabId, updatedValues) => {
    setTabs((prevContent) =>
      prevContent.map((tab) =>
        tab.tab_id === tabId ? { ...tab, ...updatedValues } : tab
      )
    );
  };

  const addTableTab = useCallback(
    async (tab) => {
      const newTab = {
        tab_id: tab.title,
        tab_title: tab.title,
        tab_type: "table",
        tab_content: tab.content,
        tab_internal_tab_selected: "details",
        order: tabs.length,
      };
      const existingTab = tabs.find((tab) => tab.tab_id === newTab.tab_id);
      if (existingTab) {
        setActiveTab(existingTab.tab_id);
        return;
      }
      await writeToDB(newTab);
      setTabs((prevTabs) => [...prevTabs, newTab]);
      setActiveTab(newTab.tab_id);
    },
    [tabs]
  );

  const updateInternalTabSelected = (tabId, selectedTab) => {
    const updatedTabs = tabs.map((tab) => {
      if (tab.tab_id === tabId) {
        return { ...tab, tab_internal_tab_selected: selectedTab };
      }
      return tab;
    });
    setTabs(updatedTabs);
  };

  const deleteTab = useCallback(
    async (tabId) => {
      if (window.confirm("Are you sure you want to close this tab?")) {
        const remainingTabs = tabs.filter((tab) => tab.tab_id !== tabId);
        setTabs(remainingTabs);
        setActiveTab("home");
        try {
          await deleteFromDB(tabId);
          toast.success("Tab Closed");
        } catch (error) {
          toast.error(`Error deleting tab ${tabId} - ${error}`);
        }
      }
    },
    [tabs]
  );

  const saveTab = async (tab) => {
    try {
      await writeToDB(tab);
      toast.success(`Tab ${tab.tab_title} saved successfully`);
    } catch (error) {
      toast.error("Error saving tab: " + error.message);
    }
  };

  const runQuery = async (tabId, query) => {
    setIsLoadingQuery(true);
    try {
      if (!query) {
        toast.error("Invalid query. Please enter a valid query.");
        return;
      }

      // if query has create or insert statements, show a warning
      const isCreateOrInsert =
        query.toLowerCase().includes("create") ||
        query.toLowerCase().includes("insert") ||
        query.toLowerCase().includes("alter") ||
        query.toLowerCase().includes("drop");
      let result;
      if (isCreateOrInsert) {
        result = await clickHouseClient.current.command({
          query,
          clickhouse_settings: {
            wait_end_of_query: 0,
          },
          format: "JSON",
        });
        const data = result;
        updateQueryTab(tabId, {
          last_run: new Date().toISOString(),
          tab_results: [
            {
              success: "true",
              message: JSON.stringify(data),
            },
          ],
          tab_results_statistics: [],
          tab_errors: null,
        });
      } else {
        result = await clickHouseClient.current.query({
          query,
          format: "JSON",
        });
        const data = await result.json();
        updateQueryTab(tabId, {
          last_run: new Date().toISOString(),
          tab_results: data.data,
          tab_results_statistics: data.statistics,
          tab_errors: null,
        });
      }
    } catch (error) {
      toast.error("Error running query: " + error.message);
      updateQueryTab(tabId, {
        last_run: new Date().toISOString(),
        tab_results: null,
        tab_results_statistics: null,
        tab_errors: error.message,
      });
    } finally {
      setIsLoadingQuery(false);
    }
  };

  const value = {
    tabs,
    setTabs,
    activeTab,
    setActiveTab,
    isLoading,
    setIsLoading,
    addTableTab,
    addQueryTab,
    deleteTab,
    saveTab,
    updateInternalTabSelected,
    updateQueryTab,
    runQuery,
    isLoadingQuery,
  };

  return (
    <TabStateContext.Provider value={value}>
      {children}
    </TabStateContext.Provider>
  );
};

export const useTabState = () => useContext(TabStateContext);
