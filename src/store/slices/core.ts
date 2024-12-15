import { StateCreator } from "zustand";
import { AppState, CoreSlice, Credential } from "@/types/common";
import { ClickHouseSettings, createClient } from "@clickhouse/client-web";
import { toast } from "sonner";
import { isCreateOrInsert } from "@/helpers/sqlUtils";
import * as IndexedDB from "@/lib/indexDB";
import { OverflowMode } from "@clickhouse/client-common/dist/settings";

const buildConnectionUrl = (credential: Credential): string => {
  let baseUrl = credential.url;

  // Remove trailing slashes from the base URL
  baseUrl = baseUrl.replace(/\/+$/, "");

  // If using advanced settings with a custom path
  if (credential.useAdvanced && credential.customPath) {
    // Remove leading slash from custom path if it exists
    const cleanPath = credential.customPath.replace(/^\/+/, "");
    return `${baseUrl}/${cleanPath}`;
  }

  return baseUrl;
};

export const createCoreSlice: StateCreator<AppState, [], [], CoreSlice> = (
  set,
  get
) => ({
  credential: {
    url: "",
    username: "",
    password: "",
    useAdvanced: false,
    customPath: "",
  },
  clickHouseClient: null,
  isLoadingCredentials: false,
  isServerAvailable: false,
  isInitialized: false,
  version: "",
  error: "",
  credentialSource: null,
  clickhouseSettings: {
    max_result_rows: "0",
    max_result_bytes: "0",
    result_overflow_mode: "break" as OverflowMode,
  },

  setCredentialSource: (source) => set({ credentialSource: source }),

  setCredential: async (credential) => {
    set({ credential, isLoadingCredentials: true });
    try {
      const connectionUrl = buildConnectionUrl(credential);

      const client = createClient({
        url: connectionUrl,
        pathname: credential.customPath, // Use custom path for proxy
        username: credential.username,
        password: credential.password || "",
        clickhouse_settings: {
          ...get().clickhouseSettings,
          result_overflow_mode: "break",
        },
      });

      set({ clickHouseClient: client });
      await get()
        .checkServerStatus()
        .then(() => {
          get().checkIsAdmin();
        });
    } catch (error) {
      set({ error: (error as Error).message });
      toast.error(`Failed to set credentials: ${(error as Error).message}`);
    } finally {
      set({ isLoadingCredentials: false });
    }
  },

  updateConfiguration: async (clickhouseSettings: ClickHouseSettings) => {
    try {
      const credentials = get().credential;
      const connectionUrl = buildConnectionUrl(credentials);

      const client = createClient({
        url: connectionUrl,
        pathname: credentials.customPath, // Ensure custom path is applied
        username: credentials.username,
        password: credentials.password || "",
        clickhouse_settings: clickhouseSettings,
      });

      set({ clickHouseClient: client, clickhouseSettings });
      await get().checkServerStatus();
    } catch (error) {
      throw error;
    }
  },

  clearCredentials: async () => {
    set({
      credential: {
        url: "",
        username: "",
        password: "",
        useAdvanced: false,
        customPath: "",
      },
      clickhouseSettings: {
        max_result_rows: "0",
        max_result_bytes: "0",
        result_overflow_mode: "break",
      },
      clickHouseClient: null,
      isServerAvailable: false,
      version: "",
      error: "",
    });
  },

  checkServerStatus: async () => {
    const { clickHouseClient } = get();
    set({ isLoadingCredentials: true, error: "" });
    try {
      if (!clickHouseClient) {
        throw new Error("ClickHouse client is not initialized");
      }
      await clickHouseClient.ping();
      const versionResult = await clickHouseClient.query({
        query: "SELECT version()",
      });
      const versionData = (await versionResult.json()) as {
        data: { "version()": string }[];
      };
      const version = versionData.data[0]["version()"];
      set({ isServerAvailable: true, version });
    } catch (error: any) {
      let errorMessage = error.message;
      if (error.response?.status === 404) {
        errorMessage =
          "Unable to connect to the specified cluster. Please check your custom path configuration.";
      } else if (error.response?.status === 502) {
        errorMessage =
          "Proxy error: Unable to reach the ClickHouse server. Please verify your cluster is running.";
      }
      set({ isServerAvailable: false, error: errorMessage });
      // Clear credentials when connection fails
      await get().clearCredentials();
      // Clear IndexedDB storage
      toast.error("Connection failed. Credentials have been cleared.");
    } finally {
      set({ isLoadingCredentials: false });
    }
  },

  checkIsAdmin: async () => {
    try {
      const result = await get().runQuery(
        "SELECT * FROM system.settings WHERE name = 'access_management'"
      );
      if (result?.data?.[0]?.value === "1") {
        set({ isAdmin: true });
      } else {
        set({ isAdmin: false });
      }
    } catch (error) {
      set({ isAdmin: false });
      console.error("Error checking admin status:", error);
    }
  },

  runQuery: async (query: string, tabId?: string) => {
    const { clickHouseClient } = get();
    if (!clickHouseClient) {
      throw new Error("ClickHouse client is not initialized");
    }

    // Set initial loading state
    if (tabId) {
      set((state) => ({
        tabs: state.tabs.map((tab) =>
          tab.id === tabId ? { ...tab, isLoading: true, error: null } : tab
        ),
      }));
    }

    try {
      const trimmedQuery = query.trim();

      if (isCreateOrInsert(trimmedQuery)) {
        await clickHouseClient.command({ query: trimmedQuery });
        const result = {
          meta: [],
          data: [],
          statistics: {},
          rows: 0,
          error: null,
        };
        if (tabId)
          await get().updateTab(tabId, {
            result,
            isLoading: false,
            error: null,
          });
        return result;
      }

      const result = await clickHouseClient.query({ query: trimmedQuery });
      const jsonResult = await result.json();
      const processedResult = {
        meta: jsonResult.meta || [],
        data: jsonResult.data || [],
        statistics: jsonResult.statistics || {},
        rows: jsonResult.rows || 0,
        error: null,
      };

      if (tabId)
        await get().updateTab(tabId, {
          result: processedResult,
          isLoading: false,
        });
      return processedResult;
    } catch (error: any) {
      const errorResult = {
        meta: [],
        data: [],
        statistics: {},
        rows: 0,
        error: error.message,
      };
      if (tabId)
        await get().updateTab(tabId, {
          result: errorResult,
          isLoading: false,
          error: error.message,
        });
      return errorResult;
    } finally {
      if (tabId) {
        set((state) => ({
          tabs: state.tabs.map((tab) =>
            tab.id === tabId ? { ...tab, isLoading: false } : tab
          ),
        }));
      }
    }
  },

  initializeApp: async () => {
    const { credential, setCredential } = get();
    if (credential.url && credential.username) {
      await setCredential(credential);
    }

    const db = await IndexedDB.initDB();
    set({ indexDbInstance: db });

    const tabs = await IndexedDB.getTabs(db);
    set({ tabs });

    if (tabs.length === 0) {
      await get().addTab({
        id: "home",
        title: "Home",
        content: "",
        type: "home",
      });
    }

    set({ isInitialized: true });
  },
});
