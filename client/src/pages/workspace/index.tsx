import { useEffect, useState, useCallback } from "react";
import DatabaseExplorer from "@/components/workspace/DataExplorer";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import WorkspaceTabs from "@/components/workspace/WorkspaceTabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import useTabStore from "@/stores/tabs.store";
import useAuthStore from "@/stores/user.store";
import CreateTable from "@/components/CreateTable";
import CreateDatabase from "@/components/CreateDatabase";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

function WorkspacePage() {
  const navigate = useNavigate();
  const { error, resetTabs } = useTabStore();
  const { getActiveOrganization, getActiveCredential } = useAuthStore();
  const [activeOrg, setActiveOrg] = useState(getActiveOrganization());
  const [activeCred, setActiveCred] = useState(getActiveCredential());

  const handleOrgCredChange = useCallback(() => {
    const currentOrg = getActiveOrganization();
    const currentCred = getActiveCredential();

    if (
      currentOrg?._id !== activeOrg?._id ||
      currentCred?._id !== activeCred?._id
    ) {
      console.log("Organization or credential changed. Reloading workspace...");
      setActiveOrg(currentOrg);
      setActiveCred(currentCred);
      resetTabs();
      // Add any other reload logic here
      // For example, you might want to refetch the database structure:
      // fetchDatabaseStructure();
    }
  }, [
    getActiveOrganization,
    getActiveCredential,
    activeOrg,
    activeCred,
    resetTabs,
  ]);

  useEffect(() => {
    const currentOrg = getActiveOrganization();
    const currentCred = getActiveCredential();
    if (!currentOrg || !currentCred){
      navigate('/organizations')
      toast.warning("You need to have a Selected Organization and Selected Credential to access the workspace")

    }
      // Initial check
      handleOrgCredChange();

    // Subscribe to changes in the auth store
    const unsubscribe = useAuthStore.subscribe(handleOrgCredChange);

    // Cleanup subscription on component unmount
    return () => unsubscribe();
  }, [handleOrgCredChange]);

  if (error) {
    return (
      <section className="flex items-center justify-center h-screen">
        <div className="w-full max-w-md p-6 space-y-6">
          <Alert variant="destructive">
            <AlertTitle>Something went wrong</AlertTitle>
            <AlertDescription>
              <div className="items-center space-x-2">
                <p>Make sure that:</p>
                <p>1. The ClickHouse server is running.</p>
                <p>2. The connection details are correct.</p>
                <p>3. The user has the necessary permissions.</p>
                <p>4. The network connection is stable.</p>
              </div>
            </AlertDescription>
          </Alert>

          <p className="text-sm text-center text-gray-500 dark:text-gray-400">
            If the issue persists, please contact your administrator, or read{" "}
            <a
              href="https://clickhouse.com/docs/en/getting-started/quick-start?utm_source=ch-ui-app&utm_medium=error-message-workspace-page"
              target="_blank"
              rel="noreferrer"
              className="text-blue-500 hover:underline"
            >
              Click House's documentation.
            </a>
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
      <CreateTable />
      <CreateDatabase />
      <ResizablePanelGroup direction="horizontal">
        <ResizablePanel className="overflow-scroll" defaultSize={25}>
          <DatabaseExplorer />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel
          className="overflow-scroll"
          defaultSize={75}
          minSize={60}
        >
          <WorkspaceTabs />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

export default WorkspacePage;
