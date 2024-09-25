import { create } from "zustand";
import useTabStore from "./tabs.store";
import { toast } from "sonner";
import { menuItemsConfig, MenuItem, Query } from "@/lib/metricsConfig";

/**
 * Interface representing a single metric.
 */
export interface Metric {
  query: string;
  title: string;
  type: "card" | "table" | "line" | "bar-chart" | "area-chart";
  description?: string;
  data: any[];
}

/**
 * Interface representing the state of the Metrics store.
 */
interface MetricsState {
  /**
   * Array of metrics data.
   */
  metricsData: Metric[] | null;

  /**
   * Timestamp of when the metrics were last fetched.
   */
  lastFetched: number | null;

  /**
   * Sets the metrics data and updates the lastFetched timestamp.
   * @param data Array of Metric objects.
   */
  setMetricsData: (data: Metric[]) => void;

  /**
   * Clears the metrics data and resets the lastFetched timestamp.
   */
  clearMetricsData: () => void;

  /**
   * Fetches metrics based on the current path.
   * @param setLoading Function to set the loading state.
   * @param setError Function to set the error state.
   * @param currentPath The current URL path to determine which queries to run.
   */
  fetchMetrics: (
    setLoading: (loading: boolean) => void,
    setError: (error: string | null) => void,
    currentPath: string
  ) => Promise<void>;
}

/**
 * Type representing the possible return types of runQuery
 */
type QueryResult =
  | {
      data?: any[];
      error?: string;
    }
  | undefined;

/**
 * Helper function to find a MenuItem by href.
 * @param items Array of MenuItem objects.
 * @param href The href to search for.
 * @returns The matched MenuItem or undefined.
 */
function findMenuItemByHref(
  items: MenuItem[],
  href: string
): MenuItem | undefined {
  for (const item of items) {
    if (item.href === href) {
      return item;
    } else if (item.items) {
      const found = findMenuItemByHref(item.items, href);
      if (found) return found;
    }
  }
  return undefined;
}

/**
 * Zustand store for managing metrics data and caching.
 */
const useMetricsStore = create<MetricsState>((set, get) => ({
  metricsData: null,
  lastFetched: null,

  setMetricsData: (data) => set({ metricsData: data, lastFetched: Date.now() }),

  clearMetricsData: () => set({ metricsData: null, lastFetched: null }),

  fetchMetrics: async (setLoading, setError, currentPath) => {
    const { runQuery } = useTabStore.getState();
    try {
      setLoading(true);
      setError(null);

      // Find the menu item corresponding to the current path
      const matchingMenuItem = findMenuItemByHref(menuItemsConfig, currentPath);

      if (!matchingMenuItem) {
        throw new Error("No matching menu item found for the current path.");
      }

      // Determine the queries to run
      let queries: Query[] = [];

      // If the menu item has its own queries
      if (matchingMenuItem.queries && matchingMenuItem.queries.length > 0) {
        queries = matchingMenuItem.queries;
      }

      // If the menu item has sub-items with their own queries
      if (matchingMenuItem.items && matchingMenuItem.items.length > 0) {
        matchingMenuItem.items.forEach((subItem) => {
          if (subItem.queries && subItem.queries.length > 0) {
            queries = queries.concat(subItem.queries);
          }
        });
      }

      if (queries.length === 0) {
        throw new Error("No queries defined for the current path.");
      }

      // Execute all queries
      const results: QueryResult[] = await Promise.all(
        queries.map(async (q) => {
          try {
            const result = await runQuery("", q.query);
            if (!result) {
              return { data: [] };
            }
            return result;
          } catch (error) {
            return { error: String(error) };
          }
        })
      );

      // Format the results
      const formattedData = results.map((result, index) => {
        if (!result) {
          throw new Error(
            `No result returned for query "${queries[index].title}"`
          );
        }
        if ("error" in result && result.error) {
          throw new Error(
            `Error in query "${queries[index].title}": ${result.error}`
          );
        }
        return {
          ...queries[index],
          data: result.data || [],
        };
      });

      // Update the store with the fetched metrics
      get().setMetricsData(formattedData);
    } catch (err: any) {
      setError(`Failed to load metrics: ${err.message || err}`);
      toast.error(`Failed to load metrics: ${err.message || err}`);
    } finally {
      setLoading(false);
    }
  },
}));

export default useMetricsStore;
