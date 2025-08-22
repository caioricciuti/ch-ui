import { useCallback, useState, useMemo, useEffect } from "react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import {
  X,
  Plus,
  Home,
  GripVertical,
  Info,
  Terminal,
  XSquareIcon,
  Copy,
  Save,
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
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  horizontalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import HomeTab from "@/features/workspace/components/HomeTab";
import useAppStore from "@/store";
import SqlTab from "@/features/workspace/components//SqlTab";
import InformationTab from "@/features/workspace/components/infoTab/InfoTab";
import { genTabId } from "@/lib/utils";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useSearchParams } from "react-router-dom";

interface Tab {
  id: string;
  title: string;
  type: "sql" | "home" | "information";
  content: string | { database?: string; table?: string };
  isSaved?: boolean;
}

interface SortableTabProps {
  tab: Tab;
  isActive: boolean;
  onActivate: () => void;
}

function SortableTab({ tab, isActive, onActivate }: SortableTabProps) {
  const { attributes, listeners, setNodeRef, transform, transition } =
    useSortable({ id: tab.id });
  const { removeTab, duplicateTab } = useAppStore();
  const [isHovering, setIsHovering] = useState(false);

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    width: tab.type === "home" ? "100px" : "150px",
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`flex items-center ${isActive ? "z-10" : "z-0"}`}
      onMouseEnter={() => setIsHovering(true)}
      onMouseLeave={() => setIsHovering(false)}
      onAuxClick={(e) => {
        if (e.button === 1) {
          // Only handle middle mouse button
          e.preventDefault();
          removeTab(tab.id);
        }
      }}
    >
      <ContextMenu>
        <ContextMenuTrigger className="flex h-[150px] w-[300px] items-center justify-center rounded-md border border-dashed text-sm">
          <TabsTrigger
            value={tab.id}
            className={`data-[state=active]:bg-orange-500 h-8 data-[state=active]:text-primary flex items-center rounded-sm w-full`}
            onClick={onActivate}
          >
            {isActive && isHovering && tab.type !== "home" && (
              <div {...attributes} {...listeners} className="cursor-move px-1">
                <GripVertical className="cursor-move p-0" size={12} />
              </div>
            )}
            {tab.type === "home" && (
              <Home width={16} className="mr-2 min-w-4" />
            )}
            {tab.type === "sql" && !tab.isSaved && (
              <Terminal width={16} className="mr-2 min-w-4" />
            )}
            {tab.type === "sql" && tab.isSaved && (
              <Save width={16} className="mr-2 min-w-4" />
            )}
            {tab.type === "information" && (
              <Info width={16} className="mr-2 min-w-4" />
            )}

            <div className="flex items-center overflow-hidden">
              <span className="truncate max-w-16 text-xs">{tab.title}</span>
            </div>

            {tab.id !== "home" && (
              <span
                className="ml-auto cursor-pointer"
                onClick={(e) => {
                  e.stopPropagation();
                  removeTab(tab.id);
                }}
              >
                <X className="h-4 w-4" />
              </span>
            )}
          </TabsTrigger>
        </ContextMenuTrigger>

        <ContextMenuContent>
          {tab.type === "sql" && (
            <ContextMenuItem onClick={() => duplicateTab(tab.id)}>
              Duplicate Tab <Copy className="ml-4 h-4 w-4" />
            </ContextMenuItem>
          )}

          {tab.type !== "home" && (
            <ContextMenuItem
              onClick={() => removeTab(tab.id)}
              className="text-red-600"
            >
              Close Tab <XSquareIcon className="ml-4 h-4 w-4" />
            </ContextMenuItem>
          )}

          {tab.type === "home" && (
            <ContextMenuItem>
              Home Tab <Home className="ml-4 h-4 w-4" />
            </ContextMenuItem>
          )}
        </ContextMenuContent>
      </ContextMenu>
    </div>
  );
}

export function WorkspaceTabs() {
  const { tabs, activeTab, addTab, setActiveTab, moveTab, closeAllTabs } =
    useAppStore();

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  const [searchParams, setSearchParams] = useSearchParams();

  useEffect(() => {
    const database = searchParams.get("database") || "";
    const table = searchParams.get("table") || "";
    if (database || table) {
      // look if the tab already exists
      const existingTab = tabs.find(
        (tab) =>
          tab.type === "information" &&
          typeof tab.content === "object" &&
          tab.content.database === database &&
          tab.content.table === table
      );
      if (existingTab) {
        setActiveTab(existingTab.id);
      } else {
        addTab({
          id: genTabId(),
          title: `Information: ${database || table}`,
          type: "information",
          content: { database, table },
        });
      }

      // Clean up URL parameters
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, tabs]);

  const addNewCodeTab = useCallback(() => {
    addTab({
      id: genTabId(),
      title: "Query " + tabs.length,
      type: "sql",
      content: "",
    });
  }, [tabs.length, addTab]);

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (active.id !== over?.id && active.id !== "home" && over?.id !== "home") {
      const oldIndex = tabs.findIndex((tab) => tab.id === active.id);
      const newIndex = tabs.findIndex((tab) => tab.id === over?.id);
      moveTab(oldIndex, newIndex);
    }
  };

  const sortedTabs = useMemo(() => {
    const homeTab = tabs.find((tab) => tab.id === "home");
    const otherTabs = tabs.filter((tab) => tab.id !== "home");
    return homeTab ? [homeTab, ...otherTabs] : otherTabs;
  }, [tabs]);

  return (
    <div className="flex flex-col h-full">
      <Tabs
        value={activeTab || undefined}
        onValueChange={setActiveTab}
        className="flex flex-col h-full"
      >
        <div className="flex-shrink-0 flex items-center">
          <Button
            variant="link"
            className="rounded-none hover:bg-gray-200 h-8 px-2 sticky left-0 z-10 bg-background"
            onClick={addNewCodeTab}
          >
            <Plus className="h-4 w-4" />
          </Button>
          <ScrollArea className="flex-grow">
            <ContextMenu>
              <ContextMenuTrigger>
                <DndContext
                  sensors={sensors}
                  collisionDetection={closestCenter}
                  onDragEnd={handleDragEnd}
                >
                  <SortableContext
                    items={sortedTabs.map((tab) => tab.id)}
                    strategy={horizontalListSortingStrategy}
                  >
                    <div className="flex">
                      <TabsList className="inline-flex h-10 items-center justify-start rounded-none w-full overflow-y-clip">
                        {sortedTabs.map((tab) => (
                          <SortableTab
                            key={tab.id}
                            tab={
                              tab.id === "home"
                                ? { ...tab, title: "Home" }
                                : tab
                            }
                            isActive={activeTab === tab.id}
                            onActivate={() => setActiveTab(tab.id)}
                          />
                        ))}
                      </TabsList>
                    </div>
                  </SortableContext>
                </DndContext>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuItem onClick={addNewCodeTab}>
                  New Tab <Plus className="ml-4 h-4 w-4" />
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem
                  onClick={closeAllTabs}
                  className="text-red-600"
                >
                  Close All Tabs <XSquareIcon className="ml-4 h-4 w-4" />
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
            <ScrollBar orientation="horizontal" />
          </ScrollArea>
        </div>
        <div className="flex flex-col flex-1">
          {sortedTabs.map((tab) => (
            <TabsContent
              key={tab.id}
              value={tab.id}
              className="h-full p-0 outline-none data-[state=active]:block"
            >
              {tab.type === "home" ? (
                <HomeTab />
              ) : tab.type === "sql" ? (
                <SqlTab tabId={tab.id} />
              ) : tab.type === "information" ? (
                <InformationTab
                  database={
                    typeof tab.content === "object" && tab.content.database
                      ? tab.content.database
                      : ""
                  }
                  tableName={
                    typeof tab.content === "object"
                      ? tab.content.table
                      : undefined
                  }
                />
              ) : null}
            </TabsContent>
          ))}
        </div>
      </Tabs>
    </div>
  );
}

export default WorkspaceTabs;
