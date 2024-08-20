import { useState, useEffect } from "react";
import api from "../api/axios.config";
import DatabaseExplorer from "@/components/workspace/DataExplorer";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import WorkspaceTabs from "@/components/workspace/WorkspaceTabs";
import { Skeleton } from "@/components/ui/skeleton";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import useAuthStore from "@/stores/user.store";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

function WorkspacePage() {
  const { setSelectedCredential } = useClickHouseCredentialStore();
  const { user } = useAuthStore();
  const [databaseData, setDatabaseData] = useState([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchDatabaseData = async () => {
    try {
      setIsLoading(true);
      const response = await api.get("/ch-queries/databases");
      setDatabaseData(response.data);
      setError(null);
    } catch (err) {
      setError("Failed to fetch database data. Please try again later.");
      console.error("Error fetching database data:", err);
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchDatabaseData();
  }, [setSelectedCredential, user?.activeClickhouseCredential]);

  if (isLoading) {
    return (
      <div className="skeleton-container p-6">
        <Skeleton className="skeleton-heading" />
        <Skeleton className="skeleton-square" />
        <div className="skeleton-grid">
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
          <Skeleton className="skeleton-item" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <section className="flex items-center justify-center h-screen">
        <div className="w-full max-w-md p-6 space-y-6">
          <Alert variant="destructive">
            <AlertTitle>Something went wrong</AlertTitle>
            <AlertDescription>
              {/* Stepts to take to resolve the issue */}
              <div className=" items-center space-x-2">
                Make sure that: 1. The ClickHouse server is running. 2. The
                connection details are correct. 3. The user has the necessary
                permissions. 4. The network connection is stable.
              </div>
            </AlertDescription>
          </Alert>

          <Button
            variant="default"
            className="w-full"
            onClick={fetchDatabaseData}
          >
            Retry
          </Button>

          <p className="text-sm text-center text-gray-500 dark:text-gray-400">
            If the issue persists, please contact support.
          </p>

          <details className="text-xs text-gray-500 dark:text-gray-400">
            <summary className="cursor-pointer">Error Details</summary>
            <pre className="mt-2 p-2 bg-gray-100 dark:bg-gray-800 rounded overflow-x-auto">
              {error}
            </pre>
          </details>
        </div>
      </section>
    );
  }

  return (
    <div className="h-screen">
      <ResizablePanelGroup direction="horizontal">
        <ResizablePanel className="overflow-scroll" defaultSize={25}>
          <DatabaseExplorer
            reloadDatabases={fetchDatabaseData}
            data={databaseData}
          />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel className="overflow-scroll" defaultSize={75}>
          <WorkspaceTabs />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

export default WorkspacePage;
