import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
} from "react";
import { createClient } from "@clickhouse/client-web";
import { toast } from "sonner";

const ClickHouseContext = createContext();

const ClickHouseProvider = ({ children }) => {
  const clickHouseClient = useRef(null);
  const [credentials, setCredentials] = useState(
    () => JSON.parse(localStorage.getItem("chCredentials")) || {},
  );
  const [isServerAvailable, setIsServerAvailable] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [version, setVersion] = useState("");
  const [error, setError] = useState("");
  const [clusterOverviewData, setClusterOverviewData] = useState([]);

  const checkServerStatus = useCallback(async () => {
    try {
      const availability = await clickHouseClient.current.ping();
      if (!availability.success) {
        toast.error("ClickHouse server is not available");
      }
      const versionResponse = await clickHouseClient.current.query({
        query: "SELECT version()",
        format: "JSONEachRow",
      });
      const versionData = await versionResponse.json();
      setVersion(versionData[0]["version()"] || "Unknown");
      setIsServerAvailable(true);
      toast.success("ClickHouse server is available");
    } catch (error) {
      setIsServerAvailable(false);
      setError(`Server status check failed: ${error.message}`);
      setVersion("-");
      setClusterOverviewData([]);
      toast.error(`Server status check failed: ${error.message}`);
    }
  }, []);

  useEffect(() => {
    const initializeClient = async () => {
      try {
        clickHouseClient.current = createClient({
          url: credentials.url,
          username: credentials.username,
          password: credentials.password,
        });
        await checkServerStatus();
      } catch (error) {
        setError(`Initialization error: ${error.message}`);
      } finally {
        setIsLoading(false);
      }
    };
    if (credentials.url && credentials.username) {
      initializeClient();
    } else {
      setIsLoading(false);
    }
  }, [credentials, checkServerStatus]);

  const clusterOverview = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await clickHouseClient.current.query({
        query: "SELECT * FROM system.clusters",
        format: "JSONEachRow",
      });
      const data = await response.json();
      setClusterOverviewData(data);
    } catch (error) {
      setError(`Cluster overview fetch failed: ${error.message}`);
      setClusterOverviewData([]);
      toast.error(error.message);
    } finally {
      setIsLoading(false);
    }
  }, []);

  const value = {
    clickHouseClient,
    isServerAvailable,
    isLoading,
    setIsLoading,
    version,
    clusterOverviewData,
    clusterOverview,
    credentials,
    setCredentials,
    error,
    setError,
  };

  return (
    <ClickHouseContext.Provider value={value}>
      {children}
    </ClickHouseContext.Provider>
  );
};

const useClickHouseState = () => {
  const context = useContext(ClickHouseContext);
  if (!context) {
    throw new Error(
      "useClickHouseState must be used within a ClickHouseProvider",
    );
  }
  return context;
};

export { ClickHouseProvider, useClickHouseState };
