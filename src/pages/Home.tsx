import { useEffect } from "react";
import DatabaseExplorer from "@/features/explorer/components/DataExplorer";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import WorkspaceTabs from "@/features/workspace/components//WorkspaceTabs";
import CreateTable from "@/features/explorer/components/CreateTable";
import CreateDatabase from "@/features/explorer/components/CreateDatabase";
import UploadFromFile from "@/features/explorer/components/UploadFile";

function HomePage() {
  useEffect(() => {
    document.title = "CH-UI | Home - Workspace";
  }, []);

  return (
    <div className="h-screen w-full overflow-auto">
      <CreateTable />
      <CreateDatabase />
      <UploadFromFile />
      <ResizablePanelGroup direction="horizontal">
        <ResizablePanel className="overflow-scroll" defaultSize={25}>
          <DatabaseExplorer />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel
          className="overflow-scroll"
          defaultSize={75}
          minSize={40}
        >
          <WorkspaceTabs />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

export default HomePage;
