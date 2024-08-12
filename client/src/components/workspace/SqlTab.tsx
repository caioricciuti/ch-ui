import React, { useEffect, useRef, useMemo } from "react";
import * as monaco from "monaco-editor";
import { Table } from "@/components/workspace/ResultTable";
import { ColumnDef } from "@tanstack/react-table";
import { useTheme } from "@/components/theme-provider";

import {
  ResizablePanel,
  ResizablePanelGroup,
  ResizableHandle,
} from "@/components/ui/resizable";
import useTabStore from "@/stores/tabs.store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/lib/monacoConfig";
import { Button } from "@/components/ui/button";
import { Play, Save } from "lucide-react";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { FlipWords } from "@/components/ui/flip-words";

interface SQLEditorProps {
  tabId: string;
}

interface ClickHouseQueryResult {
  meta: { name: string; type: string }[];
  data: Record<string, any>[];
  rows: number;
  statistics: {
    elapsed: number;
    rows_read: number;
    bytes_read: number;
  };
}

const QueryResults: React.FC<{
  results: ClickHouseQueryResult | null;
  isLoading: boolean;
  error: string | null;
}> = ({ results, isLoading, error }) => {
  const columns = useMemo(() => {
    if (!results || !results.meta) return [];
    return results.meta.map((column) => ({
      accessorKey: column.name,
      header: column.name,
    })) as ColumnDef<Record<string, any>>[];
  }, [results]);

  if (isLoading)
    return (
      <>
        <div className="p-4">
          <FlipWords
            className="text-xl font-bold m-auto"
            words={[
              "Running Query...",
              "Please wait...",
              "It's take a long time...",
            ]}
            duration={2000}
          />
          <div className="skeleton-container p-6">
            <Skeleton className="skeleton-heading" />
            <Skeleton className="skeleton-square" />
          </div>
        </div>
      </>
    );
  if (error) return null;
  if (!results || results.data.length === 0) return null;

  return (
    <div className="h-full flex flex-col p-4 overflow-hidden">
      <div className="flex-grow overflow-auto">
        <Table data={results.data} columns={columns} initialPageSize={20} />
      </div>
      <div className="mb-4 text-sm flex mt-4 justify-between font-thin">
        <p>Rows: {results.rows}</p>
        <p>Elapsed Time: {results.statistics.elapsed.toFixed(6)} seconds</p>
        <p>Rows Read: {results.statistics.rows_read}</p>
        <p>Bytes Read: {results.statistics.bytes_read}</p>
      </div>
    </div>
  );
};

export const SQLEditor: React.FC<SQLEditorProps> = ({ tabId }) => {
  const { runQuery, getTabById, updateTabContent } = useTabStore();
  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);

  const tab = getTabById(tabId);

  const sysTheme = useTheme().theme;

  const theme =
    sysTheme === "system" || sysTheme === "dark" ? "vs-dark" : "vs-light";

  useEffect(() => {
    initializeMonacoGlobally();
    const editor = createMonacoEditor(editorRef.current!, theme);
    monacoRef.current = editor;

    // Set initial content
    if (tab && tab.content) {
      editor.setValue(tab.content);
    }

    // Add change listener to update content
    const changeListener = editor.onDidChangeModelContent(() => {
      const newContent = editor.getValue();
      updateTabContent(tabId, { content: newContent });
    });

    return () => {
      changeListener.dispose();
      editor.dispose();
    };
  }, [tabId, updateTabContent]);

  if (!tab) {
    return null;
  }

  const handleRunQuery = () => {
    runQuery(tabId, tab.content);
  };

  return (
    <div className="h-[calc(100vh-theme(spacing.32))] px-2">
      <ResizablePanelGroup direction="vertical">
        <ResizablePanel defaultSize={50} minSize={0}>
          <div className="p-1 flex items-center">
            <span className="text-xs text-gray-400 truncate">{tab.title}</span>
            <Button
              size="sm"
              variant="ghost"
              className="ml-auto text-xs"
              onClick={handleRunQuery}
            >
              Run Query <Play height="14" />
            </Button>
            <Popover>
              <PopoverTrigger>
                <Button size="sm" variant="ghost" className="text-xs">
                  Save <Save height="14" />
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-max p-2 flex flex-col">
                <Button variant="link">Save Query</Button>
                <Button variant="link">Create View</Button>
              </PopoverContent>
            </Popover>
          </div>
          <div ref={editorRef} className="h-[calc(100%-2rem)]" />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={50} minSize={0}>
          <QueryResults
            results={tab.results as unknown as ClickHouseQueryResult | null}
            isLoading={tab.isLoading || false}
            error={tab.error || null}
          />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
};

export default SQLEditor;
