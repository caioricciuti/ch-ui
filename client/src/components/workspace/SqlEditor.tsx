// SqlEditor.tsx
import React, { useEffect, useRef } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/theme-provider";
import useTabStore from "@/stores/tabs.store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/lib/monacoConfig";
import { Button } from "@/components/ui/button";
import { Play } from "lucide-react";

interface SQLEditorProps {
  tabId: string;
}

const SQLEditor: React.FC<SQLEditorProps> = ({ tabId }) => {
  const { runQuery, getTabById, updateTabContent } = useTabStore();
  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const tab = getTabById(tabId);
  const { theme } = useTheme();

  const editorTheme = theme === "dark" ? "vs-dark" : "vs-light";

  useEffect(() => {
    initializeMonacoGlobally();
    if (editorRef.current) {
      const editor = createMonacoEditor(editorRef.current, editorTheme);
      monacoRef.current = editor;

      if (tab?.content) {
        editor.setValue(tab.content);
      }

      const changeListener = editor.onDidChangeModelContent(() => {
        const newContent = editor.getValue();
        updateTabContent(tabId, { content: newContent });
      });

      return () => {
        changeListener.dispose();
        editor.dispose();
      };
    }
  }, [tabId, updateTabContent, editorTheme]);

  if (!tab) {
    return null;
  }

  const handleRunQuery = () => {
    if (monacoRef.current) {
      const content = monacoRef.current.getValue();
      runQuery(tabId, content);
    }
  };

  return (
    <div className="h-full flex flex-col">
      <div className="p-1 flex items-center justify-between">
        <span className="text-xs text-gray-400 truncate">{tab.title}</span>
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
