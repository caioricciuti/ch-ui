import React, { useEffect, useRef, useCallback } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/theme-provider";
import useAppStore from "@/store/appStore";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/helpers/monacoConfig";
import { Button } from "@/components/ui/button";
import { Play } from "lucide-react";

interface SQLEditorProps {
  tabId: string;
  onRunQuery: (query: string) => void;
}

const SQLEditor: React.FC<SQLEditorProps> = ({ tabId, onRunQuery }) => {
  const { getTabById, updateTab } = useAppStore();
  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const tab = getTabById(tabId);
  const { theme } = useTheme();

  const editorTheme = theme === "light" ? "vs-light" : "vs-dark";

  useEffect(() => {
    initializeMonacoGlobally();
    if (editorRef.current) {
      const editor = createMonacoEditor(editorRef.current, editorTheme);
      monacoRef.current = editor;

      if (tab?.content) {
        const content = tab.content;
        editor.setValue(content);
      }

      const changeListener = editor.onDidChangeModelContent(() => {
        const newContent = editor.getValue();
        updateTab(tabId, { content: newContent });
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
  }, [tabId, updateTab, editorTheme]);

  const handleRunQuery = useCallback(() => {
    if (monacoRef.current) {
      const content = getSelectedText() || monacoRef.current.getValue();
      console.log("Editor: Running query:", content);
      onRunQuery(content);
    }
  }, [onRunQuery]);

  const getSelectedText = () => {
    if (monacoRef.current) {
      const model = monacoRef.current.getModel();
      if (model) {
        const selection = monacoRef.current.getSelection();
        if (selection && !selection.isEmpty()) {
          return model.getValueInRange(selection);
        }
      }
    }
    return null;
  };

  if (!tab) return null;

  return (
    <div className="h-full flex flex-col">
      <div className="p-1 flex items-center justify-between">
        <span className="ml-4 text-xs text-gray-400 truncate">{tab.title}</span>
        <Button
          size="sm"
          variant="ghost"
          className="text-xs"
          onClick={handleRunQuery}
        >
          Run Query <Play className="ml-1" height={12} />
        </Button>
      </div>
      <div ref={editorRef} className="h-[calc(100%-2rem)]" />
    </div>
  );
};

export default SQLEditor;
