import DatabaseExplorer from "@/components/explorer/DataExplorer";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import WorkspaceTabs from "@/components/tabs/WorkspaceTabs";
import CreateTable from "@/components/explorer/CreateTable";
import CreateDatabase from "@/components/explorer/CreateDatabase";

function HomePage() {
  return (
    <div className="h-screen w-full overflow-auto">
      <CreateTable />
      <CreateDatabase />
      <ResizablePanelGroup direction="horizontal">
        <ResizablePanel className="overflow-scroll" defaultSize={25}>
          <DatabaseExplorer />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel className="overflow-scroll" defaultSize={75}>
          <WorkspaceTabs />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

export default HomePage;
