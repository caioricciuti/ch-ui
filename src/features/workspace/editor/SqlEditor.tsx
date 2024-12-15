import React, { useEffect, useRef, useCallback } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/common/theme-provider";
import useAppStore from "@/store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/features/workspace/editor/monacoConfig";
import { Button } from "@/components/ui/button";
import { CirclePlay } from "lucide-react";
import { toast } from "sonner";
import SaveQueryDialog from "@/features/workspace/editor/SaveQuery";

interface SQLEditorProps {
  tabId: string;
  onRunQuery: (query: string) => void;
}

const SQLEditor: React.FC<SQLEditorProps> = ({ tabId, onRunQuery }) => {
  const {
    getTabById,
    updateTab,
    savedQueries: { isSavedQueriesActive },
    checkSavedQueriesStatus,
  } = useAppStore();

  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const tab = getTabById(tabId);
  const { theme } = useTheme();

  const editorTheme = theme === "light" ? "vs-light" : "vs-dark";

  // Check saved queries status when component mounts
  useEffect(() => {
    checkSavedQueriesStatus();
  }, []);

  useEffect(() => {
    initializeMonacoGlobally();
    if (editorRef.current) {
      const editor = createMonacoEditor(editorRef.current, editorTheme);
      monacoRef.current = editor;

      if (tab?.content) {
        const content = typeof tab.content === "string" ? tab.content : "";
        editor.setValue(content);
      }

      const changeListener = editor.onDidChangeModelContent(() => {
        const newContent = editor.getValue();
        updateTab(tabId, { content: newContent });
      });

      // Add keyboard shortcuts
      editor.addCommand(
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter,
        handleRunQuery
      );
      editor.addCommand(
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS,
        handleSaveShortcut
      );

      return () => {
        changeListener.dispose();
        editor.dispose();
      };
    }
  }, [tabId, updateTab, editorTheme]);

  const getCurrentQuery = useCallback(() => {
    if (monacoRef.current) {
      const selection = monacoRef.current.getSelection();
      const model = monacoRef.current.getModel();

      if (selection && model && !selection.isEmpty()) {
        return model.getValueInRange(selection);
      }
      return monacoRef.current.getValue();
    }
    return "";
  }, []);

  const handleRunQuery = useCallback(() => {
    const content = getCurrentQuery();
    if (content.trim()) {
      onRunQuery(content);
    } else {
      toast.error("Please enter a query to run");
    }
  }, [onRunQuery, getCurrentQuery]);

  const handleSaveShortcut = useCallback(
    (e?: any) => {
      e?.preventDefault();
      const content = getCurrentQuery();
      if (!content.trim()) {
        toast.error("Please enter a query to save");
        return;
      }
      if (!isSavedQueriesActive) {
        toast.error("Saved queries feature is not activated");
        return;
      }
      // The dialog handles the actual saving
    },
    [getCurrentQuery, isSavedQueriesActive]
  );

  const handleSaveSuccess = useCallback(() => {
    // Update tab title if it's a new query
    if (tab?.title.startsWith("New Query")) {
      updateTab(tabId, {
        title: `Saved Query - ${new Date().toLocaleTimeString()}`,
      });
    }
    toast.success("Query saved successfully!");
  }, [tabId, tab?.title, updateTab]);

  if (!tab) return null;

  return (
    <div className="h-full flex flex-col">
      <div className="px-4 flex items-center justify-between border-b">
        <span className="text-sm text-muted-foreground truncate max-w-[200px]">
          {tab.title}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="link" onClick={handleRunQuery} className="gap-2">
            <CirclePlay className="h-6 w-6" />
          </Button>
        </div>
      </div>
      <div ref={editorRef} className="flex-1" />
    </div>
  );
};

export default SQLEditor;
