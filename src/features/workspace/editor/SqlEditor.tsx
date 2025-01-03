import React, { useEffect, useRef, useCallback, useState } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/common/theme-provider";
import useAppStore from "@/store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/features/workspace/editor/monacoConfig";
import { Button } from "@/components/ui/button";
import { CirclePlay, Edit3Icon } from "lucide-react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";

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
  const [isEditing, setIsEditing] = useState(false);
  const [editedTitle, setEditedTitle] = useState("");

  const editorTheme = theme === "light" ? "vs-light" : "vs-dark";

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

  const handleTitleEdit = () => {
    setEditedTitle(tab?.title || "");
    setIsEditing(true);
  };

  const handleTitleSave = () => {
    if (editedTitle.trim()) {
      updateTab(tabId, { title: editedTitle.trim() });
    }
    setIsEditing(false);
  };

  const handleTitleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setEditedTitle(e.target.value);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      handleTitleSave();
    } else if (e.key === "Escape") {
      setIsEditing(false);
    }
  };

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

  if (!tab) return null;

  return (
    <div className="h-full flex flex-col">
      <div className="px-4 flex items-center justify-between border-b">
        <div className="flex items-center gap-2">
          {isEditing ? (
            <Input
              type="text"
              value={editedTitle}
              autoFocus
              className="w-full h-6"
              onChange={handleTitleChange}
              onBlur={handleTitleSave}
              onKeyDown={handleKeyDown}
            />
          ) : (
            <span className="text-sm text-muted-foreground truncate max-w-[200px]">
              {tab.title}
            </span>
          )}
          <Edit3Icon
            className="h-4 w-4 cursor-pointer hover:text-primary"
            onClick={handleTitleEdit}
          />
        </div>
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
