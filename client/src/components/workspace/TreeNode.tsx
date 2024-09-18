// TreeNode.tsx
import React, { useState, useCallback, useMemo } from "react";
import {
  ChevronRight,
  ChevronDown,
  Database,
  Table,
  FileSpreadsheet,
  Code2,
  Folder,
  Eye,
  Trash,
  TerminalIcon,
  MoreVertical,
  FilePlus,
  BookOpen,
  FolderPlus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import ConfirmationDialog from "@/components/ConfirmationDialog";
import { toast } from "sonner";
import useTabStore from "@/stores/tabs.store";
import CreateTable from "@/components/CreateTable";

export interface TreeNodeData {
  name: string;
  type: "database" | "table" | "view" | "saved_query" | "folder";
  children?: TreeNodeData[];
  query?: string;
}

interface TreeNodeProps {
  node: TreeNodeData;
  level: number;
  searchTerm: string;
  parentDatabaseName?: string;
  refreshData: () => void;
}

const TreeNode: React.FC<TreeNodeProps> = ({
  node,
  level,
  searchTerm,
  parentDatabaseName,
  refreshData,
}) => {
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
        content: { query: "", database, table },
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

  const getIcon = useMemo(() => {
    switch (node.type) {
      case "database":
        return <Database className="w-4 h-4 mr-2" />;
      case "table":
        return <Table className="w-4 h-4 mr-2" />;
      case "view":
        return <FileSpreadsheet className="w-4 h-4 mr-2" />;
      case "saved_query":
        return <Code2 className="w-4 h-4 mr-2" />;
      case "folder":
        return <Folder className="w-4 h-4 mr-2" />;
      default:
        return null;
    }
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
        refreshData();
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
        refreshData();
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
          label: "Create Database",
          icon: <FolderPlus className="w-4 h-4 mr-2" />,
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
          // Using non-null assertion since parentDatabaseName should be defined for table
          action: parentDatabaseName
            ? handleQueryData(parentDatabaseName, node.name)
            : () => {
                toast.error("Parent database name is undefined.");
              },
        },
        {
          label: "Delete",
          icon: <Trash className="w-4 h-4 mr-2" />,
          // Using non-null assertion since parentDatabaseName should be defined for table
          action: parentDatabaseName
            ? () => actionDropTable(parentDatabaseName, node.name)
            : () => {
                toast.error("Parent database name is undefined.");
              },
        },
      ],
      view: [
        {
          label: "Query View",
          icon: <TerminalIcon className="w-4 h-4 mr-2" />,
          // Using non-null assertion since parentDatabaseName should be defined for view
          action: parentDatabaseName
            ? handleQueryData(parentDatabaseName, node.name)
            : () => {
                toast.error("Parent database name is undefined.");
              },
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
    [
      parentDatabaseName,
      node.name,
      handleQueryData,
      actionDropDatabase,
      actionDropTable,
      handleOpenSavedQuery,
    ]
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
              {getIcon}
              <div
                onClick={() => {
                  if (node.type === "table" || node.type === "view") {
                    // Using non-null assertion since parentDatabaseName should be defined for table/view
                    if (parentDatabaseName) {
                      openInfoTab(parentDatabaseName, node.name);
                    } else {
                      toast.error("Parent database name is undefined.");
                    }
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
                  ].map((option, index) => (
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
          {contextMenuOptions[node.type as keyof typeof contextMenuOptions].map(
            (option, index) => (
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
                  refreshData={refreshData}
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
};

export default TreeNode;
