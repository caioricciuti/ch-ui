import React, { useState, useMemo, useCallback } from "react";
import {
  ChevronRight,
  ChevronDown,
  Database,
  Table,
  FileSpreadsheet,
  Search,
  SearchX,
  FilePlus,
  Trash,
  Download,
  Eye,
  MoreVertical,
  RefreshCcw,
  TerminalIcon,
  Code2,
  Folder,
  BookOpen,
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
import ConfirmationDialog from "@/components/ConfirmationDialog";

export type NodeType = "database" | "table" | "view" | "saved_query" | "folder";

export interface TreeNodeData {
  name: string;
  type: NodeType;
  children?: TreeNodeData[];
  starred?: boolean;
  query?: string; // Added for saved_query
}

interface TreeNodeProps {
  node: TreeNodeData;
  level: number;
  searchTerm: string;
  parentDatabaseName?: string;
}

interface DatabaseExplorerProps {
  data: TreeNodeData[];
  reloadDatabases?: () => void;
}

const TreeNode: React.FC<TreeNodeProps> = React.memo(
  ({ node, level, searchTerm, parentDatabaseName }) => {
    const [isOpen, setIsOpen] = useState(false);
    const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);
    const [confirmAction, setConfirmAction] = useState<() => Promise<void>>(
      () => async () => {}
    );
    const [confirmTitle, setConfirmTitle] = useState("");
    const [confirmDescription, setConfirmDescription] = useState("");
    const { addTab, runQuery, getTabById } = useTabStore();

    const toggleOpen = useCallback((e: React.MouseEvent) => {
      e.stopPropagation();
      setIsOpen((prev) => !prev);
    }, []);

    const openInfoTab = (database: string, table: string) => {
      const title = `${database}${table ? `.${table}` : ""}`;
      const existingTab = getTabById(title);

      if (existingTab) {
        toast.warning("A tab with this information is already open");
      } else {
        addTab({
          type: "information",
          title: title,
          content: { query: "", database, table }, // Added query to content
        });
      }
    };

    const handleQueryData = useCallback(
      (database: string, table: string) => async () => {
        const query = `SELECT * FROM ${database}.${table} LIMIT 1000`;
        const title = `Query - ${table}`;
        const existingTab = getTabById(title);

        if (existingTab) {
          toast.warning("A tab with this query is already open");
        } else {
          addTab({
            type: "sql",
            title: title,
            content: { query, database: database || "", table },
          });
        }
      },
      [addTab, getTabById]
    );

    const handleOpenSavedQuery = (node: TreeNodeData) => {
      const title = `Query - ${node.name}`;
      const existingTab = getTabById(title);

      if (existingTab) {
        toast.warning("A tab with this query is already open");
      } else {
        addTab({
          type: "sql",
          title: title,
          content: { query: node.query || "", database: "", table: "" },
        });
      }
    };

    const getIcon = useCallback(() => {
      if (node.type === "database")
        return <Database className="w-4 h-4 mr-2" />;
      if (node.type === "table") return <Table className="w-4 h-4 mr-2" />;
      if (node.type === "view")
        return <FileSpreadsheet className="w-4 h-4 mr-2" />;
      if (node.type === "saved_query")
        return <Code2 className="w-4 h-4 mr-2" />; // Icon for saved query
      if (node.type === "folder") return <Folder className="w-4 h-4 mr-2" />;
      return null;
    }, [node.type]);

    const actionDropDatabase = async (database: string) => {
      setConfirmTitle(`Drop Database ${database}`);
      setConfirmDescription(
        `Are you sure you want to drop the database ${database}? This action cannot be undone.`
      );
      setConfirmAction(() => async () => {
        try {
          await runQuery("", `DROP DATABASE ${database}`);
          toast.success(`Dropped database ${database}`);
        } catch (error) {
          toast.error(`Failed to drop database ${database}`);
        }
      });
      setIsConfirmDialogOpen(true);
    };

    const actionDropTable = async (database: string, table: string) => {
      setConfirmTitle(`Drop Table ${table}`);
      setConfirmDescription(
        `Are you sure you want to drop the table ${database}.${table}? This action cannot be undone.`
      );
      setConfirmAction(() => async () => {
        try {
          await runQuery("", `DROP TABLE ${database}.${table}`);
          toast.success(`Dropped table ${table}`);
        } catch (error) {
          toast.error(`Failed to drop table ${table}`);
        }
      });
      setIsConfirmDialogOpen(true);
    };

    const contextMenuOptions = useMemo(
      () => ({
        database: [
          {
            label: "View Info",
            icon: <Eye className="w-4 h-4 mr-2" />,
            action: () => openInfoTab(node.name, ""),
          },
          {
            label: "Create Table",
            icon: <FilePlus className="w-4 h-4 mr-2" />,
            action: () => console.log("Create new table"),
          },
          {
            label: "Delete",
            icon: <Trash className="w-4 h-4 mr-2" />,
            action: () => actionDropDatabase(node.name),
          },
        ],
        table: [
          {
            label: "Query Table",
            icon: <TerminalIcon className="w-4 h-4 mr-2" />,
            action: handleQueryData(parentDatabaseName, node.name),
          },
          {
            label: "Download JSON",
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
            label: "Query View",
            icon: <TerminalIcon className="w-4 h-4 mr-2" />,
            action: handleQueryData(parentDatabaseName, node.name),
          },
          {
            label: "Delete",
            icon: <Trash className="w-4 h-4 mr-2" />,
            action: () => console.log("Delete view"),
          },
        ],
        folder: [
          {
            label: "Open All",
            icon: <BookOpen className="w-4 h-4 mr-2" />,
            action: () => console.log("Open all"),
          },
        ],
        saved_query: [
          {
            label: "Open Query",
            icon: <TerminalIcon className="w-4 h-4 mr-2" />,
            action: () => handleOpenSavedQuery(node),
          },
          {
            label: "Delete",
            icon: <Trash className="w-4 h-4 mr-2" />,
            action: () => console.log("Delete saved query"),
          },
        ],
      }),
      [parentDatabaseName, node.name, handleQueryData]
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
      <>
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
                <div
                  onClick={() => {
                    if (node.type === "table" || node.type === "view") {
                      openInfoTab(parentDatabaseName!, node.name);
                    }
                    if (node.type === "saved_query") {
                      handleOpenSavedQuery(node);
                    }
                  }}
                  className="text-sm w-full"
                >
                  {node.name}
                </div>
              </div>
              <div className="flex items-center">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="icon" variant="ghost" className="h-6 w-6">
                      <MoreVertical className="w-4 h-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent>
                    {contextMenuOptions[
                      node.type as keyof typeof contextMenuOptions
                    ].map(
                      (
                        option: {
                          label: string;
                          icon: JSX.Element;
                          action: () => void;
                        },
                        index: number
                      ) => (
                        <DropdownMenuItem key={index} onSelect={option.action}>
                          {option.icon}
                          {option.label}
                        </DropdownMenuItem>
                      )
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
          </ContextMenuTrigger>
          <ContextMenuContent>
            {contextMenuOptions[
              node.type as keyof typeof contextMenuOptions
            ].map(
              (
                option: {
                  label: string;
                  icon: JSX.Element;
                  action: () => void;
                },
                index: number
              ) => (
                <ContextMenuItem key={index} onSelect={option.action}>
                  {option.icon}
                  {option.label}
                </ContextMenuItem>
              )
            )}
          </ContextMenuContent>
          {(isOpen || searchTerm) && node.children && (
            <div>
              {node.children.length > 0 ? (
                node.children.map((child, index) => (
                  <TreeNode
                    key={index}
                    node={child}
                    level={level + 1}
                    searchTerm={searchTerm}
                    parentDatabaseName={
                      node.type === "database" ? node.name : parentDatabaseName
                    }
                  />
                ))
              ) : (
                <div className="ml-6 pl-4 text-xs italic text-muted-foreground">
                  Nothing to show
                </div>
              )}
            </div>
          )}
        </ContextMenu>
        <ConfirmationDialog
          isOpen={isConfirmDialogOpen}
          variant="danger"
          onClose={() => setIsConfirmDialogOpen(false)}
          onConfirm={async () => {
            await confirmAction();
            setIsConfirmDialogOpen(false);
          }}
          title={confirmTitle}
          description={confirmDescription}
          confirmText="Delete"
          cancelText="Cancel"
        />
      </>
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
