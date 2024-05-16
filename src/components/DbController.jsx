import { useState } from "react";
import { useTabState } from "@/providers/TabsStateContext";
import { useDatabaseTablesState } from "@/providers/DatabasesTablesContext";
import { Progress } from "@/components/ui/progress";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

import {
  FileSpreadsheetIcon,
  Loader2,
  RefreshCcw,
  SearchXIcon,
  Table,
  PlusIcon,
} from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export default function DbController() {
  const { addTableTab, addQueryTab } = useTabState();
  const {
    selectedDatabase,
    changeSelectedDatabase,
    availableDatabases,
    isLoading,
    availableTables,
    loadDatabases,
    getTablesFromDatabase,
    loadingProgress,
  } = useDatabaseTablesState();

  const [searchQuery, setSearchQuery] = useState("");

  if (isLoading) {
    return (
      <>
        <div className="mt-8 flex items-center justify-center flex-col">
          <div className="flex items-center">
            <Loader2 size={26} className="animate-spin" />
            <span className="ml-4 text-xs font-thin">
              Loading tables for {selectedDatabase}
            </span>
          </div>
          <Progress value={loadingProgress} className="w-[90%] mt-6" />
          {loadingProgress > 0 && (
            <div className="ml-4">
              <span className="text-xs">{loadingProgress}%</span>
            </div>
          )}
        </div>
      </>
    );
  }

  return (
    <div className="px-4 min-w-[280px]">
      <fieldset className="rounded-lg border p-4 overflow-auto max-h-[80vh] min-w-20 mt-6">
        <legend className="-ml-1 px-1 text-sm font-medium">Databases</legend>
        <div className="flex justify-center items-center">
          <Select
            value={selectedDatabase}
            onValueChange={(value) => {
              changeSelectedDatabase(value);
            }}
            className="p-0"
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {!isLoading &&
                availableDatabases.map((database) => (
                  <SelectItem key={database.name} value={database.name}>
                    {database.name}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
          <Button
            variant="ghost"
            className="p-0 ml-2 hover:text-primary"
            onClick={() => {
              loadDatabases();
            }}
          >
            <RefreshCcw size={16} />
          </Button>
        </div>
      </fieldset>
      {availableDatabases && availableTables.length > 0 ? (
        <fieldset
          key={availableTables.name}
          className="rounded-lg border p-4 overflow-auto max-h-[80vh] min-w-20 mt-6"
        >
          <legend className="text-sm font-medium">
            Tables - ({availableTables.length})
          </legend>
          <div className="flex gap-3 items-center mb-4 mt-2">
            <Input
              type="text"
              placeholder="Search tables..."
              className="p-2 text-xs h-8 w-full"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
            <Button
              variant="ghost"
              className="p-0 hover:text-primary"
              onClick={() => {
                getTablesFromDatabase(selectedDatabase, true);
              }}
            >
              <RefreshCcw size={16} className="p-0" />
            </Button>
          </div>
          {availableTables.filter((table) =>
            table.name.toLowerCase().includes(searchQuery.toLowerCase()),
          ).length > 0 ? (
            availableTables
              .filter((table) =>
                table.name.toLowerCase().includes(searchQuery.toLowerCase()),
              )
              .map((table) => (
                <Collapsible key={table.name}>
                  <div className="flex flex-col  space-x-4 px-4 border mb-1 rounded-md">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center truncate">
                        {table.engine === "MergeTree" ? (
                          <Table className="text-primary" size={16} />
                        ) : (
                          <FileSpreadsheetIcon
                            className="text-yellow-500"
                            size={20}
                          />
                        )}
                        <Button
                          className="flex items-center text-mute font-sm p-0 truncate"
                          variant="link"
                          onClick={() => {
                            addTableTab({
                              title: table.name,
                              content: table,
                            });
                          }}
                        >
                          <span className="ml-2 text-sm truncate">
                            {table.name}
                          </span>
                        </Button>
                      </div>
                      <CollapsibleTrigger>
                        <PlusIcon size={16} />
                      </CollapsibleTrigger>
                    </div>
                    <CollapsibleContent className="mb-2 max-h-[200px] overflow-auto space-y-2 rounded-md">
                      <Button
                        variant="link"
                        className="text-xs"
                        onClick={() => {
                          addQueryTab({
                            tab_title: `Query - ${table.name} - UNIQUE`,
                            tab_content: `SELECT DISTINCT(COUNT(*)) FROM ${table.database}.${table.name};`,
                          });
                        }}
                      >
                        Count Unique
                      </Button>
                      {table.schema?.length > 0 ? (
                        table.schema.map((col) => (
                          <div
                            key={col.name}
                            className="flex items-center justify-between"
                          >
                            <span className="text-xs mt-1 px-4">
                              {col.name}
                            </span>
                            <span className="text-xs px-1 rounded-md bg-secondary mr-2">
                              {col.type}
                            </span>
                          </div>
                        ))
                      ) : (
                        <div className="text-xs m-auto text-center">
                          No schema found
                        </div>
                      )}
                    </CollapsibleContent>
                  </div>
                </Collapsible>
              ))
          ) : (
            <p className="text-sm text-gray-500 text-center">
              <SearchXIcon className="m-auto" /> No tables found
            </p>
          )}
        </fieldset>
      ) : (
        <p className="text-sm text-gray-500 text-center mt-8">
          No tables found in the selected database
        </p>
      )}
    </div>
  );
}
