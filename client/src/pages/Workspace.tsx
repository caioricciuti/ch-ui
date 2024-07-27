import { useState, useEffect } from "react";
import api from "../api/axios.config";
import { TreeNodeData } from "@/components/workspace/DataExplorer"; // Make sure this path is correct
import WorkspaceTabs from "@/components/workspace/WorkspaceTabs"; // Make sure this path is correct

function WorkspacePage() {
  const [databaseData, setDatabaseData] = useState<TreeNodeData[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchDatabaseData = async () => {
      try {
        setIsLoading(true);
        const response = await api.get<TreeNodeData[]>("/ch-queries/databases");
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
  }, []);

  if (isLoading) {
    return (
      <div className="flex justify-center items-center h-full">Loading...</div>
    );
  }

  if (error) {
    return <div>Error: {error}</div>;
  }

  return (
    <div>
      <WorkspaceTabs
        {...{
          databaseData,
        }}
      />
    </div>
  );
}

export default WorkspacePage;
