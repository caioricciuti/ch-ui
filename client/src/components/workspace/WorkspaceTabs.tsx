import { useEffect, useState } from "react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import { Input } from "@/components/ui/input";
import {
  X,
  Plus,
  Home,
  Code,
  MoreVertical,
  GripVertical,
  Info,
  Edit2,
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import SQLEditor from "@/components/workspace/SqlTab";
import HomeTab from "@/components/workspace/HomeTab";
import useTabStore from "@/stores/tabs.store";

interface SortableTabProps {
  tab: {
    id: string;
    title: string;
    type: "sql" | "result" | "home" | "information";
    content: string;
  };
  isActive: boolean;
  onActivate: () => void;
}

function SortableTab({ tab, isActive, onActivate }: SortableTabProps) {
  const { attributes, listeners, setNodeRef, transform, transition } =
    useSortable({ id: tab.id });
  const { closeTab, updateTabTitle } = useTabStore();
  const [isEditing, setIsEditing] = useState(false);
  const [editedTitle, setEditedTitle] = useState(tab.title);

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  const handleTitleClick = (e: React.MouseEvent) => {
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
    <div ref={setNodeRef} style={style} className="flex items-center">
      <TabsTrigger
        value={tab.id}
        className={`data-[state=active]:bg-orange-500 h-10 data-[state=active]:text-primary flex items-center rounded-none`}
        onClick={onActivate}
      >
        {isActive && (
          <div {...attributes} {...listeners} className="cursor-pointer px-1">
            <GripVertical className="cursor-move" size={12} />
          </div>
        )}
        {tab.type === "home" && <Home className="w-4 h-4 mr-2" />}
        {tab.type === "sql" && <Code className="w-4 h-4 mr-2" />}
        {tab.type === "information" && <Info className="w-4 h-4 mr-2" />}
        {isEditing ? (
          <Input
            value={editedTitle}
            onChange={handleTitleChange}
            onBlur={handleTitleBlur}
            onKeyDown={handleKeyDown}
            className="w-24 h-6 px-1 py-0 text-sm"
            autoFocus
          />
        ) : (
          <>
            <span onClick={handleTitleClick}>
              <Edit2 className="h-3 hidden hover:flex" />
              {tab.title}
            </span>
          </>
        )}
        {tab.id !== "home" && (
          <>
            <span
              className="ml-2 cursor-pointer"
              onClick={(e) => {
                e.stopPropagation();
                closeTab(tab.id);
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
                <DropdownMenuItem onClick={() => closeTab(tab.id)}>
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
  const { tabs, activeTabId, addTab, setActiveTab, moveTab } = useTabStore();

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  const addNewCodeTab = () => {
    addTab({
      title: "New Query",
      type: "sql",
      content: "",
    });
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;

    if (active.id !== over?.id) {
      const oldIndex = tabs.findIndex((tab) => tab.id === active.id);
      const newIndex = tabs.findIndex((tab) => tab.id === over?.id);
      moveTab(oldIndex, newIndex);
    }
  };

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.ctrlKey && event.key === "t") {
        event.preventDefault();
        addNewCodeTab();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  return (
    <div className="flex flex-col h-full">
      <Tabs
        value={activeTabId || undefined}
        onValueChange={setActiveTab}
        className="flex flex-col h-full"
      >
        <div className="flex-shrink-0 flex items-center">
          <Button
            variant="link"
            className="rounded-none hover:bg-gray-200 h-10 px-2 sticky left-0 z-10 bg-background"
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
                items={tabs.map((tab) => tab.id)}
                strategy={horizontalListSortingStrategy}
              >
                <div className="flex">
                  <TabsList className="inline-flex h-10 items-center justify-start rounded-none w-full overflow-y-clip">
                    {tabs.map((tab) => (
                      <SortableTab
                        key={tab.id}
                        tab={tab}
                        isActive={activeTabId === tab.id}
                        onActivate={() => setActiveTab(tab.id)}
                      />
                    ))}
                    <div></div>
                  </TabsList>
                </div>
              </SortableContext>
            </DndContext>
            <ScrollBar orientation="horizontal" />
          </ScrollArea>
        </div>
        <div className="overflow-hidden flex flex-col">
          {tabs.map((tab) => (
            <TabsContent
              key={tab.id}
              value={tab.id}
              className="h-full p-0 outline-none data-[state=active]:block"
            >
              <div className="h-full p-4">
                {tab.type === "home" ? (
                  <HomeTab />
                ) : tab.type === "sql" ? (
                  <SQLEditor tabId={tab.id} />
                ) : (
                  <div>Information Tab Content</div>
                )}
              </div>
            </TabsContent>
          ))}
        </div>
      </Tabs>
    </div>
  );
}

export default WorkspaceTabs;
