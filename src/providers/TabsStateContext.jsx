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
      saveTab(newTab);
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

    // Timeout function
    const timeout = (ms) =>
      new Promise((_, reject) =>
        setTimeout(() => reject(new Error("Query timed out")), ms)
      );

    try {
      if (!query) {
        toast.error("Invalid query. Please enter a valid query.");
        return;
      }

      // Function to check if the query is a create or insert statement
      const isCreateOrInsert = (query) => {
        const lowerQuery = query.toLowerCase();
        const createTableRegex = /\bcreate\s+table\b/;
        const insertRegex = /\binsert\b/;
        const alterRegex = /\balter\b/;
        const dropTableRegex = /\bdrop\s+table\b/;
        const dropColumnRegex = /\bdrop\s+column\b/;
        const dropIndexRegex = /\bdrop\s+index\b/;
        const createDatabase = /\bcreate\s+database\b/;
        const dropDatabase = /\bdrop\s+database\b/;
        const createTableAs = /\bcreate\s+table\s+as\b/;
        const createTableEngine = /\bcreate\s+table\s+engine\b/;
        const createTableIfNotExists = /\bcreate\s+table\s+if\s+not\s+exists\b/;
        const createTableLike = /\bcreate\s+table\s+like\b/;
        const createTableMaterialized = /\bcreate\s+table\s+materialized\b/;
        const createTableTemporary = /\bcreate\s+table\s+temporary\b/;
        const createTableTemporaryEngine = /\bcreate\s+table\s+temporary\s+engine\b/;
        const createTableTemporaryIfNotExists = /\bcreate\s+table\s+temporary\s+if\s+not\s+exists\b/;
        const createTableTemporaryLike = /\bcreate\s+table\s+temporary\s+like\b/;
        const createTableTemporaryMaterialized = /\bcreate\s+table\s+temporary\s+materialized\b/;
        const createTableTemporaryAs = /\bcreate\s+table\s+temporary\s+as\b/;


        return (
          createTableRegex.test(lowerQuery) ||
          insertRegex.test(lowerQuery) ||
          alterRegex.test(lowerQuery) ||
          dropTableRegex.test(lowerQuery) ||
          dropColumnRegex.test(lowerQuery) ||
          dropIndexRegex.test(lowerQuery) ||
          createDatabase.test(lowerQuery) ||
          dropDatabase.test(lowerQuery) ||
          createTableAs.test(lowerQuery) ||
          createTableEngine.test(lowerQuery) ||
          createTableIfNotExists.test(lowerQuery) ||
          createTableLike.test(lowerQuery) ||
          createTableMaterialized.test(lowerQuery) ||
          createTableTemporary.test(lowerQuery) ||
          createTableTemporaryEngine.test(lowerQuery) ||
          createTableTemporaryIfNotExists.test(lowerQuery) ||
          createTableTemporaryLike.test(lowerQuery) ||
          createTableTemporaryMaterialized.test(lowerQuery) ||
          createTableTemporaryAs.test(lowerQuery)
        );
      };

      let result;
      let data;

      // Use Promise.race to set a timeout for the query execution
      if (isCreateOrInsert(query)) {
        result = await Promise.race([
          clickHouseClient.current.command({
            query,
            clickhouse_settings: {
              wait_end_of_query: 0,
            },
            format: "JSON",
          }),
          timeout(20000), // 20 seconds timeout
        ]);
        data = result;
      } else {
        result = await Promise.race([
          clickHouseClient.current.query({
            query,
            format: "JSON",
          }),
          timeout(20000), // 20 seconds timeout
        ]);
        data = await result.json();
      }

      // If the result is successful before timeout
      updateQueryTab(tabId, {
        last_run: new Date().toISOString(),
        tab_results: isCreateOrInsert(query)
          ? [
            {
              success: "true",
              message: JSON.stringify(data),
            },
          ]
          : data.data,
        tab_results_statistics: isCreateOrInsert(query) ? [] : data.statistics,
        tab_errors: null,
      });
    } catch (error) {
      if (error.message === "Query timed out") {
        toast.error("Query timed out after 20 seconds.");
      } else {
        toast.error("Error running query: " + error.message);
      }
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

  const closeAllTabs = useCallback(async () => {
    if (window.confirm("Are you sure you want to close all tabs?")) {
      const homeTab = tabs.find(tab => tab.tab_id === "home");
      setTabs([homeTab]);
      setActiveTab("home");
      try {
        const tabsToDelete = tabs.filter(tab => tab.tab_id !== "home");
        for (const tab of tabsToDelete) {
          await deleteFromDB(tab.tab_id);
        }
        toast.success("All tabs closed");
      } catch (error) {
        toast.error(`Error closing all tabs: ${error}`);
      }
    }
  }, [tabs]);

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
    closeAllTabs,
  };

  return (
    <TabStateContext.Provider value={value}>
      {children}
    </TabStateContext.Provider>
  );
};

export const useTabState = () => useContext(TabStateContext);
