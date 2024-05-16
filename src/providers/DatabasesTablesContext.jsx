import { createContext, useContext, useState, useEffect } from "react";
import { useClickHouseState } from "@/providers/ClickHouseContext";
import { toast } from "sonner";

import { readFromDB, writeToDB } from "@/lib/tablesIndexedDB";

const DatabasesTablesContext = createContext();

export const DatabasesTableProvider = ({ children }) => {
  const { isServerAvailable, clickHouseClient, isLoading, setIsLoading } =
    useClickHouseState();
  const [availableDatabases, setAvailableDatabases] = useState(
    JSON.parse(localStorage.getItem("availableDatabases")) || [],
  );
  const [selectedDatabase, setSelectedDatabase] = useState(
    localStorage.getItem("selectedDatabase") || "",
  );
  const [availableTables, setAvailableTables] = useState([]);
  const [tablePreview, setTablePreview] = useState([]);
  const [loadingProgress, setLoadingProgress] = useState(0);

  useEffect(() => {
    if (!isServerAvailable && !isLoading) {
      return;
    }
    if (isServerAvailable) {
      fetchDatabases();
    }
  }, [isServerAvailable]);

  const fetchDatabases = async () => {
    try {
      setIsLoading(true);
      if (availableDatabases.length > 0) {
        setAvailableDatabases(
          JSON.parse(localStorage.getItem("availableDatabases")),
        );
        await getTablesFromDatabase(selectedDatabase);
        setIsLoading(false);
        return;
      }
      await loadDatabases();
    } catch (error) {
      toast.error(`Error fetching databases: ${error.message}`);
    } finally {
      setIsLoading(false);
    }
  };

  const loadDatabases = async () => {
    setIsLoading(true);
    setLoadingProgress(0);
    try {
      const databases = await clickHouseClient.current.query({
        query: "SELECT name FROM system.databases",
        format: "JSONEachRow",
      });
      const data = await databases.json();
      localStorage.setItem("availableDatabases", JSON.stringify(data));
      setAvailableDatabases(data);
      if (data && data.length > 0) {
        toast.success("Databases loaded successfully");
        changeSelectedDatabase(selectedDatabase || data[0].name);
      }
    } catch (error) {
      toast.error(`Error loading database: ${error.message}`);
    } finally {
      setLoadingProgress(100);
      setIsLoading(false);
    }
  };

  const changeSelectedDatabase = async (database) => {
    try {
      setIsLoading(true);
      setSelectedDatabase(database);
      localStorage.setItem("selectedDatabase", database);
      await getTablesFromDatabase(database);
      toast.success(`Selected database: ${database}`);
    } catch (error) {
      toast.error(`Error changing database: ${error.message}`);
    } finally {
      setIsLoading(false);
    }
  };

  const getTablesFromDatabase = async (selectedDatabase, refresh) => {
    setIsLoading(true);
    setLoadingProgress(0);
    try {
      const cachedTables = await readFromDB(selectedDatabase);
      if (cachedTables && !refresh) {
        setAvailableTables(cachedTables);
        toast.success("Tables loaded from cache.");
        setIsLoading(false);
        return;
      }
      const tables = await clickHouseClient.current.query({
        query: `SELECT * FROM system.tables WHERE database='${selectedDatabase}'`,
        format: "JSONEachRow",
      });
      let tablesData = await tables.json();
      // if no tables found, return
      if (tablesData.length === 0) {
        toast.error("No tables found in the selected database");
        setAvailableTables([]);
        return;
      }
      const totalTables = tablesData.length;
      // for each table, fetch the schema
      for (let i = 0; i < totalTables; i++) {
        const table = tablesData[i];
        const schemaDataObj = await fetchTableSchema({
          database: selectedDatabase,
          table: table.name,
        });
        table.schema = schemaDataObj.schema; // add schema to table object
        // for each table load the progress bar by the number of tables fetched from
        const progress = ((i + 1) / totalTables) * 100;
        setLoadingProgress(Math.min(progress.toFixed(2), 100));
      }
      await writeToDB(tablesData);
      setAvailableTables(tablesData);
      toast.success("Tables loaded successfully");
    } catch (error) {
      toast.error(`Error loading tables: ${error.message}`);
    } finally {
      setLoadingProgress(100);
      setIsLoading(false);
    }
  };

  const fetchTableSchema = async ({ database, table }) => {
    setIsLoading(true);
    try {
      const tableSchema = await clickHouseClient.current.query({
        query: `DESCRIBE ${database}.${table}`,
        format: "JSONEachRow",
      });
      const schemaData = await tableSchema.json();
      // Create a new Object
      const schemaDataObj = {
        table: table,
        schema: schemaData,
      };
      return schemaDataObj;
    } catch (error) {
      toast.error(`Error fetching table schema: ${error.message}`);
    } finally {
      setIsLoading(false);
    }
  };

  const fetchTablePreview = async ({ database, table }) => {
    try {
      const existingPreview = tablePreview.find(
        (preview) => preview.table === table,
      );
      if (existingPreview) {
        return;
      }
      const tablePreviewData = await clickHouseClient.current.query({
        query: `SELECT * FROM ${database}.${table} LIMIT 100`,
        format: "JSONEachRow",
      });
      const previewData = await tablePreviewData.json();
      const previewDataObj = {
        table: table,
        previewData: previewData,
      };
      setTablePreview((prevData) => [...prevData, previewDataObj]);
      toast.success("Table preview loaded successfully");
    } catch (error) {
      toast.error(`Error fetching table preview: ${error.message}`);
    }
  };

  const value = {
    isLoading,
    availableDatabases,
    selectedDatabase,
    setSelectedDatabase,
    availableTables,
    changeSelectedDatabase,
    getTablesFromDatabase,
    fetchTableSchema,
    tablePreview,
    fetchTablePreview,
    loadDatabases,
    loadingProgress,
  };

  return (
    <DatabasesTablesContext.Provider value={value}>
      {children}
    </DatabasesTablesContext.Provider>
  );
};

export const useDatabaseTablesState = () => useContext(DatabasesTablesContext);
