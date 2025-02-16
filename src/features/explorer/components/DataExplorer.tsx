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
import { SavedQuery } from "@/types/common";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";

const DatabaseExplorer: React.FC = () => {
  const [searchTerm, setSearchTerm] = useState("");
  const [searchQueryValue, setSearchQueryValue] = useState("");
  const {
    dataBaseExplorer,
    tabError,
    fetchDatabaseInfo,
    isLoadingDatabase,
    addTab,
    openCreateDatabaseModal,
    openCreateTableModal,
    openUploadFileModal,
    checkSavedQueriesStatus,
    fetchSavedQueries,
  } = useAppStore();

  const updatedSavedQueriesTrigger = useAppStore(state => state.updatedSavedQueriesTrigger);

  const [isQueriesEnabled, setIsQueriesEnabled] = useState(false);
  const [savedQueriesList, setSavedQueriesList] = useState<SavedQuery[]>([]);

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

  const filteredQueries = useMemo(() => {
    if (!searchQueryValue) return savedQueriesList;
    return savedQueriesList.filter((query) =>
      query.name.toLowerCase().includes(searchQueryValue.toLowerCase())
    );
  }, [savedQueriesList, searchQueryValue]);

  const refreshDatabases = useCallback(() => {
    fetchDatabaseInfo();
  }, [fetchDatabaseInfo]);

  const loadSavedQueries = useCallback(async () => {
    try {
      const result = await fetchSavedQueries();
      if (result) {
        setSavedQueriesList(result);
      } else {
        console.warn("No saved queries found or invalid response.");
        setSavedQueriesList([]);
      }
    } catch (error) {
      console.error("Error fetching saved queries:", error);
      setSavedQueriesList([]);
    }
  }, [fetchSavedQueries]);

  useEffect(() => {
    fetchDatabaseInfo();

    const checkQueriesEnabled = async () => {
      const enabled = await checkSavedQueriesStatus();
      setIsQueriesEnabled(enabled);
      return enabled;
    };

    checkQueriesEnabled().then((enabled) => {
      if (enabled) {
        loadSavedQueries();
      }
    });
  }, [fetchDatabaseInfo, checkSavedQueriesStatus, loadSavedQueries]);

  useEffect(() => {
    if (isQueriesEnabled) {
      loadSavedQueries();
    }
  }, [isQueriesEnabled, loadSavedQueries, updatedSavedQueriesTrigger]);

  const handleSavedQueryOpen = (query: SavedQuery) => {
    addTab({
      id: query.id,
      type: "saved_query",
      title: query.name,
      content: query.query,
    });
  };

  return (
    <div className="flex flex-col h-full">
      {/* Header Section */}
      <div className="flex-none p-4 border-b">
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
            placeholder="Search your resources"
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

      {/* Main Content Area - Databases */}
      <div className="flex-1 min-h-0 flex flex-col">
        <ScrollArea className="flex-1">
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

        {/* Saved Queries Section - Limited to 30% height with scroll */}
        {isQueriesEnabled && savedQueriesList && (
          <div
            className="flex-none border-t max-h-[30%]"
            data-testid="saved-queries"
          >
            <Accordion type="single" collapsible>
              <AccordionItem value="saved-queries">
                <div className="flex items-center justify-between px-1">
                  <AccordionTrigger className="p-4 cursor-pointer">
                    <h3 className="flex items-center justify-between truncate mr-2">
                      Saved Queries
                    </h3>
                  </AccordionTrigger>
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={loadSavedQueries}
                  >
                    <RefreshCcw className="w-4 h-4" />
                  </Button>
                </div>
                <AccordionContent>
                  <div className="px-4 pb-2">
                    <div className="relative">
                      <Search className="w-4 h-4 absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground" />
                      <Input
                        type="text"
                        placeholder="Search saved queries"
                        value={searchQueryValue}
                        onChange={(e) => setSearchQueryValue(e.target.value)}
                        className="pl-9 pr-9 py-2 w-full h-8 text-sm mt-2"
                      />
                      {searchQueryValue && (
                        <Button
                          size="icon"
                          variant="ghost"
                          onClick={() => setSearchQueryValue("")}
                          className="absolute right-2 top-1/2 transform -translate-y-1/2 h-6 w-6"
                        >
                          <SearchX className="w-4 h-4" />
                        </Button>
                      )}
                    </div>
                  </div>
                  <ScrollArea className="h-full h-64">
                    <div className="px-2">
                      {filteredQueries.length > 0 ? (
                        filteredQueries.map((query) => (
                          <div
                            key={query.id}
                            className="flex space-x-2 flex-col py-.5 w-full"
                            onClick={() => handleSavedQueryOpen(query)}
                          >
                            <div className="text-xs text-muted-foreground cursor-pointer hover:bg-muted-foreground/10 rounded-md p-1 w-full truncate flex justify-between">
                              <span className="flex-1">{query.name}</span>
                              <span className="text-xs text-muted-foreground">
                                {new Date(query.updated_at).toLocaleString()}
                              </span>
                            </div>
                          </div>
                        ))
                      ) : (
                        <div className="p-4 text-muted-foreground text-xs text-center">
                          No saved queries found
                        </div>
                      )}
                    </div>
                  </ScrollArea>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          </div>
        )}
      </div>
    </div>
  );
};

export default DatabaseExplorer;
