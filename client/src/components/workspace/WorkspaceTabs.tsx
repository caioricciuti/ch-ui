import React, { useState, useRef, useEffect } from "react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import { X, Plus, Home, Database, Code, MoreVertical } from "lucide-react";
import { DragDropContext, Droppable, Draggable } from "react-beautiful-dnd";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import DatabaseExplorer from "@/components/workspace/DataExplorer";
import { TreeNodeData } from "@/components/workspace/DataExplorer";
import SQLEditor from "./SQLEditor";

interface Tab {
  id: string;
  title: string;
  type: "info" | "code" | "result" | "home";
  content: React.ReactNode;
}

export function WorkspaceTabs({
  databaseData,
}: {
  databaseData: TreeNodeData[];
}) {
  const [tabs, setTabs] = useState<Tab[]>([
    { id: "home", title: "Home", type: "home", content: <HomeTab /> },
  ]);
  const [activeTab, setActiveTab] = useState("home");
  const scrollRef = useRef<HTMLDivElement>(null);

  const addTab = (newTab: Tab) => {
    setTabs([...tabs, newTab]);
    setActiveTab(newTab.id);
  };

  const closeTab = (tabId: string) => {
    const newTabs = tabs.filter((tab) => tab.id !== tabId);
    setTabs(newTabs);
    if (activeTab === tabId) {
      setActiveTab(newTabs[newTabs.length - 1]?.id || "home");
    }
  };

  const addNewCodeTab = () => {
    const newTabId = `code-${Date.now()}`;
    addTab({
      id: newTabId,
      title: "New Query",
      type: "code",
      content: <CodeTab initialCode="" />,
    });
  };

  const onDragEnd = (result: any) => {
    if (!result.destination) return;
    const newTabs = Array.from(tabs);
    const [reorderedItem] = newTabs.splice(result.source.index, 1);
    newTabs.splice(result.destination.index, 0, reorderedItem);
    setTabs(newTabs);
  };

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.ctrlKey && event.key === "w") {
        event.preventDefault();
        if (activeTab !== "home") {
          closeTab(activeTab);
        }
      }
      if (event.ctrlKey && event.key === "t") {
        event.preventDefault();
        addNewCodeTab();
      }
      if (event.ctrlKey && event.key >= "1" && event.key <= "9") {
        event.preventDefault();
        const tabIndex = parseInt(event.key) - 1;
        if (tabIndex < tabs.length) {
          setActiveTab(tabs[tabIndex].id);
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [tabs, activeTab]);

  return (
    <div className="flex h-screen-[-h-16]">
      <div className="w-64">
        <DatabaseExplorer
          data={databaseData}
          onNodeSelect={(node) => {
            addTab({
              id: `info-${node.id}`,
              title: node.name,
              type: "info",
              content: <InfoTab node={node} />,
            });
          }}
        />
      </div>
      <div className="flex-1 flex flex-col">
        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          className="flex-1 flex flex-col"
        >
          <div className="flex items-center">
            <ScrollArea className="flex-1" ref={scrollRef}>
              <DragDropContext onDragEnd={onDragEnd}>
                <Droppable droppableId="tabs" direction="horizontal">
                  {(provided) => (
                    <TabsList
                      className="inline-flex h-10 items-center justify-start rounded-none px-2"
                      {...provided.droppableProps}
                      ref={provided.innerRef}
                    >
                      {tabs.map((tab, index) => (
                        <Draggable
                          key={tab.id}
                          draggableId={tab.id}
                          index={index}
                        >
                          {(provided) => (
                            <TabsTrigger
                              value={tab.id}
                              className="data-[state=active]:bg-secondary data-[state=active]:text-white px-4 py-2 flex items-center"
                              ref={provided.innerRef}
                              {...provided.draggableProps}
                              {...provided.dragHandleProps}
                            >
                              {tab.type === "home" && (
                                <Home className="w-4 h-4 mr-2" />
                              )}
                              {tab.type === "info" && (
                                <Database className="w-4 h-4 mr-2" />
                              )}
                              {tab.type === "code" && (
                                <Code className="w-4 h-4 mr-2" />
                              )}
                              {tab.title}
                              {tab.id !== "home" && (
                                <>
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    className="ml-2 h-4 w-4 p-0"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      closeTab(tab.id);
                                    }}
                                  >
                                    <X className="h-3 w-3" />
                                  </Button>
                                  <DropdownMenu>
                                    <DropdownMenuTrigger asChild>
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        className="ml-1 p-0"
                                      >
                                        <MoreVertical className="h-4 w-4" />
                                      </Button>
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent>
                                      <DropdownMenuItem
                                        onClick={() => closeTab(tab.id)}
                                      >
                                        Close
                                      </DropdownMenuItem>
                                      <DropdownMenuItem
                                        onClick={() =>
                                          setTabs(
                                            tabs.filter(
                                              (t) =>
                                                t.id === "home" ||
                                                t.id === tab.id
                                            )
                                          )
                                        }
                                      >
                                        Close Others
                                      </DropdownMenuItem>
                                      <DropdownMenuItem
                                        onClick={() =>
                                          setTabs([
                                            tabs[0],
                                            ...tabs.filter(
                                              (t) => t.id === tab.id
                                            ),
                                          ])
                                        }
                                      >
                                        Close to the Right
                                      </DropdownMenuItem>
                                    </DropdownMenuContent>
                                  </DropdownMenu>
                                </>
                              )}
                            </TabsTrigger>
                          )}
                        </Draggable>
                      ))}
                      {provided.placeholder}
                    </TabsList>
                  )}
                </Droppable>
              </DragDropContext>
            </ScrollArea>
            <Button
              variant="ghost"
              size="sm"
              className="mr-2"
              onClick={addNewCodeTab}
            >
              <Plus className="h-4 w-4" />
            </Button>
          </div>
          <div className="flex-1">
            {tabs.map((tab) => (
              <TabsContent
                key={tab.id}
                value={tab.id}
                className="h-full border-none p-0 outline-none data-[state=active]:flex-1"
              >
                {tab.content}
              </TabsContent>
            ))}
          </div>
        </Tabs>
      </div>
    </div>
  );
}

function HomeTab() {
  return (
    <div className="p-4 space-y-4">
      <div className="bg-gray-800 p-4 rounded-lg">
        <h2 className="text-2xl font-bold mb-2">Star us on GitHub!</h2>
        <p>
          Star us on GitHub if you like this project. It helps us a lot, and it
          also helps other people find this project.
        </p>
      </div>
      <div className="bg-gray-800 p-4 rounded-lg">
        <h2 className="text-2xl font-bold mb-2">ClickHouse</h2>
        <p>
          ClickHouse is a fast open-source OLAP database management system,
          designed for big data analytics. Ah, also, it's open-source.
        </p>
      </div>
      <div className="bg-gray-800 p-4 rounded-lg">
        <h2 className="text-2xl font-bold mb-2">Start Querying</h2>
        <p>
          Create and run queries on your ClickHouse instance. You can also save
          your queries for later use.
        </p>
        <Button
          variant="outline"
          className="mt-2"
          onClick={() => {
            /* Add logic to open a new query tab */
          }}
        >
          Let's go!
        </Button>
      </div>
    </div>
  );
}

function InfoTab({ node }: { node: TreeNodeData }) {
  return (
    <div className="p-4">
      <h2 className="text-2xl font-bold mb-4">{node.name}</h2>
      <p>Type: {node.type}</p>
      {/* Add more info as needed */}
    </div>
  );
}

function CodeTab({ initialCode = "" }: { initialCode?: string }) {
  return (
    <div className="h-full">
      <SQLEditor
        initialValue={initialCode}
        onChange={(value) => console.log("New value:", value)}
      />
    </div>
  );
}

export default WorkspaceTabs;
