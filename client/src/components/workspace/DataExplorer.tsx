import React, { useState, useMemo, useCallback } from "react";
import {
  ChevronRight,
  ChevronDown,
  Database,
  Table,
  FileSpreadsheet,
  Search,
  SearchX,
  Plus,
  FilePlus,
  Trash,
  Edit,
  Download,
  Eye,
  MoreVertical,
  RefreshCcw,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import useTabStore from "@/stores/tabs.store";
import { toast } from "sonner";

export type NodeType = "database" | "table" | "view";

export interface TreeNodeData {
  name: string;
  type: NodeType;
  children?: TreeNodeData[];
  starred?: boolean;
}

interface TreeNodeProps {
  node: TreeNodeData;
  level: number;
  searchTerm: string;
  parentDatabaseName?: string; // Added to pass the parent database name
}

interface DatabaseExplorerProps {
  data: TreeNodeData[];
  reloadDatabases?: () => void;
}

const TreeNode: React.FC<TreeNodeProps> = React.memo(
  ({ node, level, searchTerm, parentDatabaseName }) => {
    const [isOpen, setIsOpen] = useState(false);
    const { addTab, runQuery } = useTabStore();

    const toggleOpen = useCallback((e: React.MouseEvent) => {
      e.stopPropagation();
      setIsOpen((prev) => !prev);
    }, []);

    const handleViewData = useCallback(() => {
      addTab({
        title: node.name,
        type: "sql",
        content: `SELECT * FROM ${node.name};`,
      });
    }, [node.name, addTab]);

    const getIcon = useCallback(() => {
      if (node.type === "database")
        return <Database className="w-4 h-4 mr-2" />;
      if (node.type === "table") return <Table className="w-4 h-4 mr-2" />;
      if (node.type === "view")
        return <FileSpreadsheet className="w-4 h-4 mr-2" />;
      return null;
    }, [node.type]);

    const actionDropDatabase = async (database: string) => {
      if (
        window.confirm(`Are you sure you want to drop database ${database}?`)
      ) {
        console.log(`Dropping database ${database}`);
      }
      await runQuery("", `DROP DATABASE ${database}`);
      toast.success(`Dropped database ${database}`);
    };

    const actionDropTable = async (database: string, table: string) => {
      if (window.confirm(`Are you sure you want to drop table ${table}?`)) {
        try {
          await runQuery("", `DROP TABLE ${database}.${table}`);
          toast.success(`Dropped table ${table}`);
        } catch (error) {
          toast.error(`Failed to drop table ${table}`);
        }
      }
    };

    const contextMenuOptions = useMemo(
      () => ({
        database: [
          {
            label: "Create Table",
            icon: <FilePlus className="w-4 h-4 mr-2" />,
            action: () => console.log("Create new table"),
          },
          {
            label: "New View",
            icon: <Plus className="w-4 h-4 mr-2" />,
            action: () => console.log("Create new view"),
          },
          {
            label: "Delete",
            icon: <Trash className="w-4 h-4 mr-2" />,
            action: () => actionDropDatabase(node.name),
          },
        ],
        table: [
          {
            label: "View Data",
            icon: <Eye className="w-4 h-4 mr-2" />,
            action: () => console.log("View table data"),
          },
          {
            label: "Edit Structure",
            icon: <Edit className="w-4 h-4 mr-2" />,
            action: () => console.log("Edit table structure"),
          },
          {
            label: "Export",
            icon: <Download className="w-4 h-4 mr-2" />,
            action: () => console.log("Export table"),
          },
          {
            label: "Delete",
            icon: <Trash className="w-4 h-4 mr-2" />,
            action: () => actionDropTable(parentDatabaseName!, node.name),
          },
        ],
        view: [
          {
            label: "Edit Query",
            icon: <Edit className="w-4 h-4 mr-2" />,
            action: () => console.log("Edit view query"),
          },
          {
            label: "View Data",
            icon: <Eye className="w-4 h-4 mr-2" />,
            action: handleViewData,
          },
          {
            label: "Delete",
            icon: <Trash className="w-4 h-4 mr-2" />,
            action: () => console.log("Delete view"),
          },
        ],
      }),
      [parentDatabaseName, node.name, handleViewData]
    );

    const matchesSearch = node.name
      .toLowerCase()
      .includes(searchTerm.toLowerCase());
    const childrenMatchSearch = node.children?.some(
      (child) =>
        child.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
        child.children?.some((grandchild) =>
          grandchild.name.toLowerCase().includes(searchTerm.toLowerCase())
        )
    );

    const shouldRender = !searchTerm || matchesSearch || childrenMatchSearch;

    return shouldRender ? (
      <ContextMenu>
        <ContextMenuTrigger>
          <div
            className={`flex items-center py-1 px-2 hover:bg-secondary hover:rounded-md cursor-pointer
              ${level > 0 ? "ml-4" : ""}`}
            onClick={toggleOpen}
          >
            <div className="flex-grow flex items-center">
              {node.children ? (
                isOpen ? (
                  <ChevronDown className="w-4 h-4 mr-1" />
                ) : (
                  <ChevronRight className="w-4 h-4 mr-1" />
                )
              ) : (
                <div className="w-4 mr-1" />
              )}
              {getIcon()}
              <p className="text-sm">{node.name}</p>
            </div>
            <div className="flex items-center">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="icon" variant="ghost" className="h-6 w-6">
                    <MoreVertical className="w-4 h-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  {contextMenuOptions[node.type].map((option, index) => (
                    <DropdownMenuItem key={index} onSelect={option.action}>
                      {option.icon}
                      {option.label}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent>
          {contextMenuOptions[node.type].map((option, index) => (
            <ContextMenuItem key={index} onSelect={option.action}>
              {option.icon}
              {option.label}
            </ContextMenuItem>
          ))}
        </ContextMenuContent>
        {(isOpen || searchTerm) && node.children && (
          <div>
            {node.children.map((child, index) => (
              <TreeNode
                key={index}
                node={child}
                level={level + 1}
                searchTerm={searchTerm}
                parentDatabaseName={
                  node.type === "database" ? node.name : parentDatabaseName
                } // Pass down database name
              />
            ))}
          </div>
        )}
      </ContextMenu>
    ) : null;
  }
);

const DatabaseExplorer: React.FC<DatabaseExplorerProps> = ({
  data,
  reloadDatabases,
}) => {
  const [searchTerm, setSearchTerm] = useState("");

  const filteredData = useMemo(() => {
    let filtered = data;
    if (!searchTerm) return filtered;
    return filtered.filter(
      (node) =>
        node.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
        node.children?.some(
          (child) =>
            child.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
            child.children?.some((grandchild) =>
              grandchild.name.toLowerCase().includes(searchTerm.toLowerCase())
            )
        )
    );
  }, [data, searchTerm]);

  const refreshDatabases = useCallback(() => {
    if (reloadDatabases) reloadDatabases();
  }, [reloadDatabases]);

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b">
        <div className="flex justify-between items-center mb-3">
          <h2 className="text-lg font-semibold">Explorer</h2>
          <Button
            size="sm"
            variant="outline"
            onClick={() => refreshDatabases()}
            className="flex items-center"
          >
            <RefreshCcw className="w-4 h-4 ml-1" />
          </Button>
        </div>
        <div className="relative mb-2">
          <Search className="w-4 h-4 absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search resources"
            value={searchTerm}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              setSearchTerm(e.target.value)
            }
            className="pl-9 pr-9 py-2 w-full"
          />
          {searchTerm && (
            <Button
              size="icon"
              variant="ghost"
              onClick={() => setSearchTerm("")}
              className="absolute right-2 top-1/2 transform -translate-y-1/2 h-6 w-6"
            >
              <SearchX className="w-4 h-4" />
            </Button>
          )}
        </div>
      </div>
      <ScrollArea>
        <div className="p-3">
          {filteredData && filteredData.length > 0 ? (
            filteredData.map((node, index) => (
              <TreeNode
                key={index}
                node={node}
                level={0}
                searchTerm={searchTerm}
                parentDatabaseName={node.name} // Pass the database name at the top level
              />
            ))
          ) : (
            <div className="p-4 text-muted-foreground w-full flex flex-col items-center justify-center">
              <SearchX className="w-8 h-8 mx-auto" />
              <p className="text-center mt-2">No results found</p>
              {searchTerm && (
                <Button
                  onClick={() => setSearchTerm("")}
                  variant="outline"
                  className="mt-4"
                >
                  Clear Search
                </Button>
              )}
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
};

export default DatabaseExplorer;
