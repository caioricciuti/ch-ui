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
import { analyticsQueries } from "@/helpers/instanceAnalyticsQueries";

// Polyfill for crypto.randomUUID if not available
if (typeof crypto.randomUUID !== "function") {
  crypto.randomUUID = function () {
    return ([1e7] + -1e3 + -4e3 + -8e3 + -1e11).replace(/[018]/g, (c) =>
      (
        c ^
        (crypto.getRandomValues(new Uint8Array(1))[0] & (15 >> (c / 4)))
      ).toString(16)
    );
  };
}

const ClickHouseContext = createContext();

const ClickHouseProvider = ({ children }) => {
  const clickHouseClient = useRef(null);
  const [credentials, setCredentials] = useState(
    () => JSON.parse(localStorage.getItem("chCredentials")) || {}
  );
  const [isServerAvailable, setIsServerAvailable] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [version, setVersion] = useState("");
  const [error, setError] = useState("");
  const [analyticsData, setAnalyticsData] = useState([]);

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

  const getDataAnalytics = useCallback(async () => {
    setIsLoading(true);

    try {
      const newAnalyticsData = await Promise.all(
        analyticsQueries.map(async (element) => {
          const response = await clickHouseClient.current.query({
            query: element.query,
            format: element.format,
          });
          const data = await response.json();
          return {
            data: data,
            title: element.title,
            plot: element.plot,
            description: element.description,
            data_format: element.data_format,
            query: element.query,
            format: element.format,
          };
        })
      );

      setAnalyticsData(newAnalyticsData);
    } catch (error) {
      setError(`Data analytics fetch failed: ${error.message}`);
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
    credentials,
    setCredentials,
    error,
    setError,
    getDataAnalytics,
    analyticsData,
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
      "useClickHouseState must be used within a ClickHouseProvider"
    );
  }
  return context;
};

export { ClickHouseProvider, useClickHouseState };
