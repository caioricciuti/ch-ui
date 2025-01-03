import React, { useCallback, useState, useMemo } from "react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import {
  X,
  Plus,
  Home,
  GripVertical,
  Info,
  Edit2,
  Terminal,
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
import { Input } from "@/components/ui/input";
import { genTabId } from "@/lib/utils";
interface Tab {
  id: string;
  title: string;
  type: "sql" | "home" | "information" | "saved_query";
  content: string | { database?: string; table?: string };
}

interface SortableTabProps {
  tab: Tab;
  isActive: boolean;
  onActivate: () => void;
}

function SortableTab({ tab, isActive, onActivate }: SortableTabProps) {
  const { attributes, listeners, setNodeRef, transform, transition } =
    useSortable({ id: tab.id });
  const { removeTab, updateTabTitle } = useAppStore();
  const [isEditing, setIsEditing] = useState(false);
  const [editedTitle, setEditedTitle] = useState(tab.title);
  const [isHovering, setIsHovering] = useState(false);

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    width: tab.type === "home" ? "100px" : "150px",
  };

  const handleEditClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    setIsEditing(true);
  };

  const handleTitleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setEditedTitle(e.target.value);
  };

  const handleTitleBlur = () => {
    if (editedTitle.trim() !== "") {
      updateTabTitle(tab.id, editedTitle);
    }
    setIsEditing(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      handleTitleBlur();
    }
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`flex items-center ${isActive ? "z-10" : "z-0"}`}
      onMouseEnter={() => setIsHovering(true)}
      onMouseLeave={() => setIsHovering(false)}
    >
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
        {tab.type === "home" && <Home width={16} className="mr-2 min-w-4" />}
        {tab.type === "sql" && <Terminal width={16} className="mr-2 min-w-4" />}
        {tab.type === "information" && (
          <Info width={16} className="mr-2 min-w-4" />
        )}
        {isEditing && tab.type !== "home" ? (
          <Input
            value={editedTitle}
            onChange={handleTitleChange}
            onBlur={handleTitleBlur}
            onKeyDown={handleKeyDown}
            className="w-24 h-4 p-1 text-xs"
          />
        ) : (
          <div className="flex items-center overflow-hidden">
            <span className="max-w-14 truncate text-xs">{tab.title}</span>
            {isActive && tab.type !== "home" && (
              <Edit2
                className="w-3 h-3 ml-1 cursor-pointer"
                onClick={handleEditClick}
              />
            )}
          </div>
        )}
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
    </div>
  );
}

export function WorkspaceTabs() {
  const { tabs, activeTab, addTab, setActiveTab, moveTab, removeTab } =
    useAppStore();

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

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

  // Sort tabs to always keep home tab first
  const sortedTabs = useMemo(() => {
    const homeTab = tabs.find((tab) => tab.id === "home");
    const otherTabs = tabs.filter((tab) => tab.id !== "home");
    return homeTab ? [homeTab, ...otherTabs] : otherTabs;
  }, [tabs]);

  /*
  ***** Keyboard shortcuts COMMENTED OUT ***** 
  useEffect(() => {
    // Keyboard shortcuts
    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "w") {
        event.preventDefault();
        if (activeTab && activeTab !== "home") {
          removeTab(activeTab);
        }
      } else if ((event.metaKey || event.ctrlKey) && event.key === "t") {
        event.preventDefault();
        addNewCodeTab();
      } else if (
        (event.metaKey || event.ctrlKey) &&
        event.key >= "1" &&
        event.key <= "9"
      ) {
        event.preventDefault();
        const tabIndex = parseInt(event.key) - 1;
        if (tabIndex < tabs.length) {
          setActiveTab(tabs[tabIndex].id);
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [tabs, activeTab, addNewCodeTab, removeTab, setActiveTab]);
*/

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
                          tab.id === "home" ? { ...tab, title: "Home" } : tab
                        }
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
