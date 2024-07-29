import React, { useState, useRef, useEffect } from "react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import {
  X,
  Plus,
  Home,
  Code,
  MoreVertical,
  Info,
  GripVertical,
} from "lucide-react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  horizontalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import SQLEditor from "@/components/workspace/SqlTab";
import HomeTab from "@/components/workspace/HomeTab";

interface Tab {
  id: string;
  title: string;
  type: "info" | "sql" | "home";
  content: React.ReactNode;
}

function SortableTab({
  tab,
  onClose,
  isActive,
  onActivate,
}: {
  tab: Tab;
  onClose: (id: string) => void;
  isActive: boolean;
  onActivate: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition } =
    useSortable({ id: tab.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div ref={setNodeRef} style={style} className="flex items-center">
      <div {...attributes} {...listeners} className="cursor-move px-2">
        <GripVertical size={16} />
      </div>
      <TabsTrigger
        value={tab.id}
        className={`data-[state=active]:bg-orange-400 h-10 data-[state=active]:text-primary flex items-center rounded-none ${
          isActive ? "bg-orange-400" : ""
        }`}
        onClick={onActivate}
      >
        {tab.type === "home" && <Home className="w-4 h-4 mr-2" />}
        {tab.type === "info" && <Info className="w-4 h-4 mr-2" />}
        {tab.type === "sql" && <Code className="w-4 h-4 mr-2" />}
        {tab.title}
        {tab.id !== "home" && (
          <>
            <span
              className="ml-2 cursor-pointer"
              onClick={(e) => {
                e.stopPropagation();
                onClose(tab.id);
              }}
            >
              <X className="h-4 w-4" />
            </span>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <span
                  className="ml-2 cursor-pointer"
                  onClick={(e) => e.stopPropagation()}
                >
                  <MoreVertical className="h-4 w-4" />
                </span>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onClick={() => onClose(tab.id)}>
                  <X className="h-4 w-4 mr-2" />
                  Close
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )}
      </TabsTrigger>
    </div>
  );
}

export function WorkspaceTabs() {
  const [tabs, setTabs] = useState<Tab[]>([
    { id: "home", title: "Home", type: "home", content: <HomeTab /> },
  ]);
  const [activeTab, setActiveTab] = useState("home");
  const scrollRef = useRef<HTMLDivElement>(null);
  const tabsListRef = useRef<HTMLDivElement | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  const addTab = (newTab: Tab) => {
    setTabs((prevTabs) => {
      const newTabs = [...prevTabs, newTab];
      return newTabs;
    });
    setActiveTab(newTab.id);
    setTimeout(() => {
      if (tabsListRef.current) {
        const tabEl = tabsListRef.current.querySelector(
          `[data-tab-id="${newTab.id}"]`
        );
        if (tabEl) {
          tabEl.scrollIntoView({ behavior: "smooth", inline: "center" });
        }
      }
    }, 100);
  };

  const closeTab = (tabId: string) => {
    setTabs((prevTabs) => {
      const newTabs = prevTabs.filter((tab) => tab.id !== tabId);
      if (activeTab === tabId) {
        const newActiveTab = newTabs[newTabs.length - 1]?.id || "home";
        setActiveTab(newActiveTab);
      }
      return newTabs;
    });
  };

  const addNewCodeTab = () => {
    const newTabId = `code-${Date.now()}`;
    addTab({
      id: newTabId,
      title: "New Query",
      type: "sql",
      content: <CodeTab initialCode="Select" />,
    });
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;

    if (active.id !== over?.id) {
      setTabs((items) => {
        const oldIndex = items.findIndex((item) => item.id === active.id);
        const newIndex = items.findIndex((item) => item.id === over?.id);

        return arrayMove(items, oldIndex, newIndex);
      });
    }
  };

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.ctrlKey && event.key === "w") {
        event.preventDefault();
        if (activeTab !== "home") {
          console.log("Ctrl+W pressed, closing active tab:", activeTab);
          closeTab(activeTab);
        }
      }
      if (event.ctrlKey && event.key === "t") {
        event.preventDefault();
        console.log("Ctrl+T pressed, adding new code tab");
        addNewCodeTab();
      }
      if (event.ctrlKey && event.key >= "1" && event.key <= "9") {
        event.preventDefault();
        const tabIndex = parseInt(event.key) - 1;
        if (tabIndex < tabs.length) {
          console.log(
            `Ctrl+${event.key} pressed, switching to tab:`,
            tabs[tabIndex].id
          );
          setActiveTab(tabs[tabIndex].id);
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [tabs, activeTab]);

  return (
    <div className="flex flex-col h-full">
      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        className="flex-1 flex flex-col"
      >
        <div className="flex items-center">
          <ScrollArea className="flex-1" ref={scrollRef}>
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              onDragEnd={handleDragEnd}
            >
              <SortableContext
                items={tabs.map((tab) => tab.id)}
                strategy={horizontalListSortingStrategy}
              >
                <div ref={tabsListRef} className="flex">
                  <TabsList className="inline-flex h-12 items-center justify-start rounded-none w-full overflow-x-hidden">
                    {tabs.map((tab) => (
                      <SortableTab
                        key={tab.id}
                        tab={tab}
                        onClose={closeTab}
                        isActive={activeTab === tab.id}
                        onActivate={() => setActiveTab(tab.id)}
                      />
                    ))}
                  </TabsList>
                </div>
              </SortableContext>
            </DndContext>
            <ScrollBar orientation="horizontal" />
          </ScrollArea>
          <Button
            variant="link"
            className="rounded-none hover:bg-gray-200"
            onClick={addNewCodeTab}
          >
            <Plus className="h-4 w-4" />
          </Button>
        </div>
        <div className="flex-1 overflow-hidden">
          {tabs.map((tab) => (
            <TabsContent
              key={tab.id}
              value={tab.id}
              className="h-[85vh] p-0 outline-none data-[state=active]:flex-1"
            >
              <ScrollArea className="h-full w-full">{tab.content}</ScrollArea>
            </TabsContent>
          ))}
        </div>
      </Tabs>
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
