import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import {
  TerminalSquareIcon,
  FilePlus2,
  HomeIcon,
  Loader2,
  SquareX,
  Table,
} from "lucide-react";
import { Skeleton } from "./ui/skeleton";
import { useTabState } from "@/providers/TabsStateContext";
import HomeTabContent from "@/TabContents/HomeTabContent";
import TableTabContent from "@/TabContents/TableTabContent";
import QueryTabContent from "@/TabContents/QueryTabContent";

export default function TabsManager() {
  const { tabs, activeTab, setActiveTab, isLoading, addQueryTab, deleteTab, queryTabContent } =
    useTabState(); // Use context to manage tabs and related state

  if (isLoading) {
    return (
      <div className="flex flex-col space-y-3 mt-8">
        <Loader2 size={26} className="animate-spin ml-4" />
        <Skeleton className="h-[15vh] w-[93%] rounded-xl" />
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      <Tabs value={activeTab} onValueChange={(e) => {
        setActiveTab(e)
      }}>
        <TabsList className="flex overflow-x-auto whitespace-nowrap dark:bg-black bg-white justify-start overflow-y-hidden relative rounded-none">
          <Button
            className="flex items-center rounded-none"
            onClick={() => addQueryTab()}
          >
            <FilePlus2 size={16} />
            <span className="ml-1 sr-only">New Tab</span>
          </Button>
          {tabs.map((tab) => (
            <TabsTrigger
              key={tab.tab_id}
              value={tab.tab_id}
              className="flex items-center first:ml-12"
            >
              {tab.tab_type === "home" ? (
                <span className="text-sm flex items-center">
                  <HomeIcon size={16} />
                </span>
              ) : tab.tab_type === "query" ? (
                <span className="text-sm flex items-center">
                  <TerminalSquareIcon size={14} className="mr-2" />
                  {tab.tab_title}
                </span>
              ) : tab.tab_type === "table" ? (
                <span className="text-sm flex items-center">
                  <Table size={14} className="mr-2" />
                  {tab.tab_title}
                </span>
              ) : (
                <span className="text-sm flex items-center">
                  {tab.tab_title}
                </span>
              )}
              {tab.tab_type !== "home" && (
                <Button
                  className="ml-2 px-0"
                  variant="link"
                  onClick={() => deleteTab(tab.tab_id)}
                >
                  <SquareX size={16} />
                </Button>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
        {tabs.map((tab) => (
          <TabsContent
            key={tab.tab_id}
            value={tab.tab_id}
            className="rounded-sm px-1"
          >
            {tab.tab_type === "query" && <QueryTabContent tab={tab} />}
            {tab.tab_type === "home" && <HomeTabContent />}
            {tab.tab_type === "table" && <TableTabContent tab={tab} />}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}