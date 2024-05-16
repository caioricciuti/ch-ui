import Tabs from "@/components/Tabs";
import DbController from "@/components/DbController";
import { DatabasesTableProvider } from "@/providers/DatabasesTablesContext";
import { TabsStateProvider } from "@/providers/TabsStateContext";
import { useClickHouseState } from "@/providers/ClickHouseContext";
import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";

export default function HomePage() {
  const navigate = useNavigate();
  const { isServerAvailable, isLoading } = useClickHouseState();

  useEffect(() => {
    if (!isServerAvailable && !isLoading) {
      navigate("/settings", { replace: true });
      toast.error("Server is not available. Please check your connection.");
      return;
    }
  }, [navigate, isServerAvailable, isLoading]); // Add isServerAvailable to the dependency array

  return (
    <DatabasesTableProvider>
      <TabsStateProvider>
        <ResizablePanelGroup
          direction="horizontal"
        >
          <ResizablePanel defaultSize={20}>
            <DbController />
          </ResizablePanel>
          <ResizableHandle withHandle={true} className="mx-1 p-0.5" />
          <ResizablePanel>
            <Tabs />
          </ResizablePanel>
        </ResizablePanelGroup>
      </TabsStateProvider>
    </DatabasesTableProvider>
  );
}
