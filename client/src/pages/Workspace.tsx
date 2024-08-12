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
    return <div>Error: {error}</div>;
  }

  return (
    <div className="h-[calc(100vh-theme(spacing.16))]">
      <ResizablePanelGroup className="h-full" direction="horizontal">
        <ResizablePanel className="overflow-scroll" defaultSize={25}>
          <DatabaseExplorer data={databaseData} />
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
