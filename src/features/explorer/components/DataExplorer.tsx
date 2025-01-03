import React, { useState, useCallback, useEffect, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  RefreshCcw,
  Search,
  SearchX,
  MoreVertical,
  FolderPlus,
  FilePlus,
  TerminalIcon,
  FileUp,
} from "lucide-react";
import useAppStore from "@/store";
import TreeNode, {
  TreeNodeData,
} from "@/features/explorer/components/TreeNode";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { genTabId } from "@/lib/utils";
const DatabaseExplorer: React.FC = () => {
  const [searchTerm, setSearchTerm] = useState("");
  const {
    dataBaseExplorer,
    tabError,
    fetchDatabaseInfo,
    isLoadingDatabase,
    addTab,
    openCreateDatabaseModal,
    openCreateTableModal,
    openUploadFileModal,
  } = useAppStore();

  const filteredData = useMemo(() => {
    if (!searchTerm) return dataBaseExplorer;
    return dataBaseExplorer.filter(
      (node) =>
        node.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
        node.children.some((child) =>
          child.name.toLowerCase().includes(searchTerm.toLowerCase())
        )
    );
  }, [dataBaseExplorer, searchTerm]);

  const refreshDatabases = useCallback(() => {
    fetchDatabaseInfo();
  }, [fetchDatabaseInfo]);

  useEffect(() => {
    fetchDatabaseInfo();
    if (dataBaseExplorer.length === 0) {
      fetchDatabaseInfo();
    }
  }, [fetchDatabaseInfo, dataBaseExplorer.length]);

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b">
        <div className="flex justify-between items-center mb-3">
          <div className="flex items-center space-x-1">
            <h2 className="text-lg font-semibold">Explorer</h2>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon" variant="ghost" className="p-0">
                  <MoreVertical className="w-4 h-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onClick={() => openCreateDatabaseModal()}>
                  <FolderPlus className="w-4 h-4 mr-2" /> Create Database
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => openCreateTableModal("")}>
                  <FilePlus className="w-4 h-4 mr-2" /> Create Table
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => openUploadFileModal("")}>
                  <FileUp className="w-4 h-4 mr-2" /> Upload File
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() =>
                    addTab({
                      id: genTabId(),
                      type: "sql",
                      title: "Query",
                      content: "",
                    })
                  }
                >
                  <TerminalIcon className="w-4 h-4 mr-2" /> New Query
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={refreshDatabases}
            className="flex items-center"
            disabled={isLoadingDatabase}
          >
            <RefreshCcw
              className={`w-4 h-4 ml-1 ${
                isLoadingDatabase ? "animate-spin" : ""
              }`}
            />
          </Button>
        </div>
        <div className="relative mb-2">
          <Search className="w-4 h-4 absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search resources"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
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
          {isLoadingDatabase ? (
            <div className="p-4 text-muted-foreground w-full flex flex-col items-center justify-center">
              <RefreshCcw className="w-8 h-8 mx-auto animate-spin" />
              <p className="text-center mt-2">Loading...</p>
            </div>
          ) : tabError ? (
            <div className="p-4 text-red-500 w-full flex flex-col items-center justify-center">
              <p className="text-center mt-2">{tabError}</p>
              <Button
                onClick={refreshDatabases}
                variant="outline"
                className="mt-4"
              >
                Retry
              </Button>
            </div>
          ) : filteredData.length > 0 ? (
            filteredData.map((node) => (
              <TreeNode
                node={node as TreeNodeData}
                level={0}
                searchTerm={searchTerm}
                parentDatabaseName={node.name}
                refreshData={refreshDatabases}
                key={node.name}
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
