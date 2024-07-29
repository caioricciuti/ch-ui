import { useState, useEffect } from "react";
import api from "../api/axios.config";
import DatabaseExplorer from "@/components/workspace/DataExplorer"; // Make sure this path is correct
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import WorkspaceTabs from "@/components/workspace/WorkspaceTabs";
import useAuthStore from "@/stores/user.store";
import { Skeleton } from "@/components/ui/skeleton";

function WorkspacePage() {
  const { setCurrentCredential } = useAuthStore();
  const [databaseData, setDatabaseData] = useState([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
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

    fetchDatabaseData();
  }, [setCurrentCredential]);

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
    <div className="h-[calc(100vh-20rem)]] w-full">
      {isLoading && <Skeleton className="h-12 w-11/12" />}
      <ResizablePanelGroup direction="horizontal">
        <ResizablePanel defaultSize={25}>
          <DatabaseExplorer data={databaseData} />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={75}>
          <WorkspaceTabs />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

export default WorkspacePage;
