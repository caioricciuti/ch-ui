import React, { useEffect, useRef, useCallback, useState } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/common/theme-provider";
import useAppStore from "@/store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/features/workspace/editor/monacoConfig";
import { Button } from "@/components/ui/button";
import { CirclePlay, Save, Trash2Icon } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  ResizablePanel,
  ResizablePanelGroup,
  ResizableHandle,
} from "@/components/ui/resizable";
import CHUITable from "@/components/common/table/CHUItable";
import { Loader2, FileX2 } from "lucide-react";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { AgGridReact } from "ag-grid-react";
import { ColDef } from "ag-grid-community";
import { AllCommunityModule } from "ag-grid-community";
import { themeBalham } from "ag-grid-community";
import { colorSchemeDark } from "ag-grid-community";


interface SavedQueryTabProps {
  tabId: string;
}

const SavedQueryTab: React.FC<SavedQueryTabProps> = ({ tabId }) => {
  const {
    getTabById,
    updateTab,
    runQuery,
    updateSavedQuery,
    fetchDatabaseInfo,
    fetchSavedQueries,
    deleteSavedQuery,
  } = useAppStore();
  const tab = getTabById(tabId);
  const { theme } = useTheme();
  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const [isSaveDialogOpen, setIsSaveDialogOpen] = useState(false);
  const [isDeleteQueryDialogOpen, setIsDeleteQueryDialogOpen] = useState(false);
  const [isDirty, setIsDirty] = useState(false);
  const [initialQuery, setInitialQuery] = useState<string>("");

  const editorTheme = theme === "light" ? "vs-light" : "vs-dark";

  // Function to set the editor value and update state
  const setEditorValue = useCallback((value: string) => {
    if (monacoRef.current) {
      monacoRef.current.setValue(value);
    }
  }, []);



  useEffect(() => {
    initializeMonacoGlobally();
    if (editorRef.current) {
      const editor = createMonacoEditor(editorRef.current, editorTheme);
      monacoRef.current = editor;

      if (tab?.content) {
        const content = typeof tab.content === "string" ? tab.content : "";
        editor.setValue(content);
        setInitialQuery(content);
      }

      const changeListener = editor.onDidChangeModelContent(() => {
        const newContent = editor.getValue();
        updateTab(tabId, { content: newContent });
        setIsDirty(newContent !== initialQuery);
      });

      editor.addCommand(
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter,
        handleRunQuery
      );

      return () => {
        changeListener.dispose();
        editor.dispose();
      };
    }
  }, [tabId, updateTab, editorTheme, initialQuery]);

  const getCurrentQuery = useCallback(() => {
    return monacoRef.current ? monacoRef.current.getValue() : "";
  }, []);

  const handleRunQuery = useCallback(async () => {
    const content = getCurrentQuery();
    if (content.trim()) {
      try {
        const shouldRefresh =
          /^\s*(CREATE|DROP|ALTER|TRUNCATE|RENAME|INSERT|UPDATE|DELETE)\s+/i.test(
            content
          );

        const result = await runQuery(content, tabId);

        if (result.error) {
          return;
        } else {
          if (shouldRefresh) {
            await fetchDatabaseInfo();
            toast.success("Data Explorer refreshed due to schema change");
          }
        }
      } catch (error) {
        console.error("Error running query:", error);
        toast.error(
          "Failed to execute query. Please check the console for more details."
        );
      }
    } else {
      toast.error("Please enter a query to run");
    }
  }, [runQuery, getCurrentQuery, tabId, fetchDatabaseInfo]);

  const handleUpdateSavedQuery = async () => {
    const content = getCurrentQuery();
    if (content.trim()) {
      try {
        await updateSavedQuery(tabId, content, tab?.title || "");
        setInitialQuery(content);
        setIsDirty(false);
        setIsSaveDialogOpen(false);
      } catch (error) {
        console.error("Error updating saved query:", error);
        toast.error(
          "Failed to update saved query. Please check the console for more details."
        );
      }
    } else {
      toast.error("Please enter a query to save");
    }
  };

  const handleDeleteSavedQuery = async (id: string) => {
    try {
      await deleteSavedQuery(id);
      setIsDeleteQueryDialogOpen(false);
    } catch (error) {
      console.error("Error deleting saved query:", error);
      toast.error(
        "Failed to delete saved query. Please check the console for more details."
      );
    }
  };

  const renderResults = () => {
    if (tab?.isLoading) {
      return (
        <div className="h-full w-full flex items-center justify-center">
          <Loader2 size={24} className="animate-spin mr-2" />
          <p>Running query</p>
        </div>
      );
    }

    if (tab?.error) {
      return (
        <div className="m-4">
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{tab.error}</AlertDescription>
          </Alert>
        </div>
      );
    }

    if (!tab?.result) {
      return (
        <div className="h-full flex items-center justify-center">
          <div className="flex flex-col items-center">
            <FileX2 size={48} className="text-muted-foreground mb-4" />
            <p className="text-sm text-muted-foreground">
              There's no data yet! Run a query to get started.
            </p>
          </div>
        </div>
      );
    }

    if (tab.result.error) {
      return (
        <div className="m-4">
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{tab.result.error}</AlertDescription>
          </Alert>
        </div>
      );
    }

    if (tab.result.message) {
      return (
        <div className="m-4">
          <Alert variant="info">
            <AlertDescription>{tab.result.message}</AlertDescription>
          </Alert>
        </div>
      );
    }

    if (
      tab.result.data &&
      tab.result.data.length > 0 &&
      tab.result.meta.length > 0
    ) {
      return (
        <div className="h-full">
          <CHUITable
            result={{
              meta: tab.result.meta,
              data: tab.result.data,
              statistics: tab.result.statistics,
              message: tab.result.message,
              query_id: tab.result.query_id,
            }}
          />
        </div>
      );
    }

    return (
      <div className="p-4 m-4 border text-sm rounded-md text-blue-600 dark:text-blue-200 bg-blue-100/30 border-blue-300">
        <p>Query executed successfully. No data returned. </p>
        <p> Elapsed time: {tab.result.statistics.elapsed} seconds.</p>{" "}
      </div>
    );
  };

  if (!tab)
    return (
      <>
        <p>Something went wrong...</p>
      </>
    );

  return (
    <div className="h-full">
      <ResizablePanelGroup direction="vertical">
        <ResizablePanel defaultSize={50} minSize={25}>
          <div className="h-full flex flex-col">
            <div className="px-4 flex items-center justify-between border-b">
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground truncate max-w-[200px]">
                  {tab.title}
                  {isDirty ? "*" : ""}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="link"
                  onClick={handleRunQuery}
                  className="gap-2"
                >
                  <CirclePlay className="h-6 w-6" />
                </Button>
                <Button
                  variant="link"
                  onClick={() => {
                    setIsSaveDialogOpen(true);
                  }}
                  className="gap-2"
                >
                  <Save className="h-4 w-4" />
                </Button>
                <Button
                  variant="link"
                  onClick={() => {
                    setIsDeleteQueryDialogOpen(true);
                  }}
                  className="gap-2"
                >
                  <Trash2Icon className="h-4 w-4" />
                </Button>
              </div>
            </div>
            <div ref={editorRef} className="flex-1" />
          </div>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={50} minSize={25}>
          {renderResults()}
        </ResizablePanel>
      </ResizablePanelGroup>

      <AlertDialog
        open={isDeleteQueryDialogOpen}
        onOpenChange={setIsDeleteQueryDialogOpen}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete saved query</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete {tab?.title}? This action
              cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => handleDeleteSavedQuery(tabId)}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={isSaveDialogOpen} onOpenChange={setIsSaveDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Update Query</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to update {tab?.title} ?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleUpdateSavedQuery}>
              Update
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
};

export default SavedQueryTab;
