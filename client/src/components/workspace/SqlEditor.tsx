import React, { useEffect, useRef, useState, useCallback } from "react";
import * as monaco from "monaco-editor";
import { useTheme } from "@/components/theme-provider";
import useTabStore from "@/stores/tabs.store";
import {
  initializeMonacoGlobally,
  createMonacoEditor,
} from "@/lib/monacoConfig";
import { Button } from "@/components/ui/button";
import { Play, Save } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import api from "@/api/axios.config";
import { toast } from "sonner";

interface SQLEditorProps {
  tabId: string;
}

const SQLEditor: React.FC<SQLEditorProps> = ({ tabId }) => {
  const { runQuery, getTabById, updateTabContent, fetchDatabaseData } =
    useTabStore();
  const editorRef = useRef<HTMLDivElement>(null);
  const monacoRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const tab = getTabById(tabId);
  const { theme } = useTheme();
  const [isSaveDialogOpen, setIsSaveDialogOpen] = useState(false);
  const [queryName, setQueryName] = useState("");
  const [isPublic, setIsPublic] = useState(false);

  const editorTheme = theme === "light" ? "vs-light" : "vs-dark";

  useEffect(() => {
    initializeMonacoGlobally();
    if (editorRef.current) {
      const editor = createMonacoEditor(editorRef.current, editorTheme);
      monacoRef.current = editor;

      if (tab?.content) {
        const content =
          typeof tab.content === "string" ? tab.content : tab.content.query;
        editor.setValue(content);
      }

      const changeListener = editor.onDidChangeModelContent(() => {
        const newContent = editor.getValue();
        updateTabContent(tabId, { content: newContent });
      });

      // add the cmd or ctrl + enter key binding to run the query
      editor.addCommand(
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter,
        handleRunQuery
      );

      return () => {
        changeListener.dispose();
        editor.dispose();
      };
    }
  }, [tabId, updateTabContent, editorTheme]);

  if (!tab) {
    return null;
  }

  const handleRunQuery = useCallback(() => {
    if (monacoRef.current) {
      const content = getSelectedText() || monacoRef.current.getValue();

      // Check if the query should trigger a refresh
      const shouldRefresh =
        /^\s*(CREATE|DROP|ALTER|TRUNCATE|RENAME|INSERT|UPDATE|DELETE)\s+/i.test(
          content
        );

      runQuery(tabId, content).then(() => {
        if (shouldRefresh) {
          fetchDatabaseData();
          toast.success("Data Explorer refreshed due to schema change");
        }
      });
    }
  }, [tabId, runQuery, fetchDatabaseData]);

  const getSelectedText = () => {
    if (monacoRef.current) {
      const model = monacoRef.current.getModel();
      if (model) {
        const selection = monacoRef.current.getSelection();
        // console.log the selected text
        if (selection && !selection.isEmpty()) {
          return model.getValueInRange(selection);
        }
      }
    }
    return null;
  };

  const handleSaveQuery = async () => {
    if (monacoRef.current) {
      const query = monacoRef.current.getValue();
      try {
        const response = await api.post("/query/save", {
          name: queryName,
          query,
          public: isPublic,
        });
        console.log("Query saved:", response.data);
        setIsSaveDialogOpen(false);
        toast.success("Query saved successfully");
      } catch (error) {
        console.error("Error saving query:", error);
        toast.error("Failed to save query. Please try again later.");
      }
    }
  };

  return (
    <div className="h-full flex flex-col">
      <div className="p-1 flex items-center justify-between">
        <span className="ml-4 text-xs text-gray-400 truncate">{tab.title}</span>
        <div className="flex space-x-2">
          <Button
            size="sm"
            variant="ghost"
            className="text-xs"
            onClick={() => setIsSaveDialogOpen(true)}
          >
            Save Query <Save className="ml-1" height={12} />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-xs"
            onClick={handleRunQuery}
          >
            Run Query <Play className="ml-1" height={12} />
          </Button>
        </div>
      </div>
      <div ref={editorRef} className="h-[calc(100%-2rem)]" />

      <Dialog open={isSaveDialogOpen} onOpenChange={setIsSaveDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Save Query</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="name" className="text-right">
                Name
              </Label>
              <Input
                id="name"
                value={queryName}
                onChange={(e) => setQueryName(e.target.value)}
                className="col-span-3"
              />
            </div>
            <div className="flex items-center space-x-2">
              <Checkbox
                id="public"
                checked={isPublic}
                onCheckedChange={(checked) => setIsPublic(checked as boolean)}
              />
              <Label htmlFor="public">Make query public</Label>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={handleSaveQuery}>Save Query</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default SQLEditor;
