import React, { useEffect, useRef, useCallback, useState } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/common/theme-provider";
import useAppStore from "@/store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/features/workspace/editor/monacoConfig";
import { Button } from "@/components/ui/button";
import { CirclePlay, Edit3Icon, Save, Clock } from "lucide-react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
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
import { useNavigate } from "react-router-dom";

interface SQLEditorProps {
  tabId: string;
  onRunQuery: (query: string) => void;
}

const SQLEditor: React.FC<SQLEditorProps> = ({ tabId, onRunQuery }) => {
  const { getTabById, updateTab, saveQuery, checkSavedQueriesStatus, isAdmin } =
    useAppStore();
  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const tab = getTabById(tabId);
  const { theme } = useTheme();
  const [isEditing, setIsEditing] = useState(false);
  const [editedTitle, setEditedTitle] = useState("");
  const [isSaveDialogOpen, setIsSaveDialogOpen] = useState(false);
  const [queryName, setQueryName] = useState(tab?.title || "Untitled Query");
  const navigate = useNavigate();

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

  const hanldeSaveOpenDialog = async () => {
    const isSavedQueryEnabled = await checkSavedQueriesStatus();
    if (!isSavedQueryEnabled) {
      isAdmin &&
        toast.warning(`Saved queries are not enable.`, {
          action: {
            label: "Enable",
            onClick: () => {
              navigate("/admin");
            },
          },
        });

      !isAdmin &&
        toast.warning(
          `Saved queries are not enable. Contact your admin to enable it.`
        );

      return;
    }
    setIsSaveDialogOpen(true);
  };

  const handleSaveQuery = async () => {
    const query = getCurrentQuery();
    if (!queryName.trim()) {
      toast.error("Please enter a query name.");
      return;
    }

    if (!query.trim()) {
      toast.error("Please enter a query to save.");
      return;
    }

    try {
      await saveQuery(tabId, queryName, query);
      setIsSaveDialogOpen(false);
    } catch (error) {
      console.error("Error saving query:", error);
      toast.error("Failed to save query.");
    }
  };

  const handleQueryNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setQueryName(e.target.value);
  };

  const handleInsertTtlSnippet = () => {
    if (monacoRef.current) {
      const editor = monacoRef.current;
      const position = editor.getPosition();
      if (position) {
        editor.executeEdits("insert-ttl-snippet", [
          {
            range: new monaco.Range(position.lineNumber, position.column, position.lineNumber, position.column),
            text: "\nTTL <date_column> + INTERVAL <number> <DAY|WEEK|MONTH|YEAR>\n",
            forceMoveMarkers: true,
          },
        ]);
        editor.focus();
      }
    }
  };

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
          <Button
            variant="link"
            onClick={hanldeSaveOpenDialog}
            className="gap-2"
            disabled={tab.type === "home" || tab.type === "information"}
          >
            <Save className="h-4 w-4" />
          </Button>
          <Button
            variant="link"
            onClick={handleInsertTtlSnippet}
            className="gap-2"
            title="Insert TTL Snippet"
          >
            <Clock className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div ref={editorRef} className="flex-1" />

      {/* Save Query Dialog */}
      <AlertDialog open={isSaveDialogOpen} onOpenChange={setIsSaveDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Save Query</AlertDialogTitle>
            <AlertDialogDescription>
              Enter a name for this query:
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Input
              type="text"
              placeholder="Query Name"
              value={queryName}
              onChange={handleQueryNameChange}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleSaveQuery}>
              {tab.isSaved ? "Update" : "Save"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
};

export default SQLEditor;
