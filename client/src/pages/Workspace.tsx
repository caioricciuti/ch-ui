import { useEffect } from "react";
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


function WorkspacePage() {
  const { error  } = useTabStore();
  const {  } = useAuthStore();

  useEffect(() => {

  }, []);

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
