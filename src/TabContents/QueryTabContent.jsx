import { useEffect, useState } from "react";
import Editor from "@monaco-editor/react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DownloadIcon,
  Loader2,
  FileSpreadsheetIcon,
  FileJson,
} from "lucide-react";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import { useTheme } from "@/providers/theme";
import { useTabState } from "@/providers/TabsStateContext";
import { AgGridReact } from "ag-grid-react";
import "ag-grid-community/styles/ag-grid.css";
import "ag-grid-community/styles/ag-theme-alpine.css";
import { format } from "sql-formatter";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import downloadCsv from "/src/helpers/donwloadCsv.js";
import { suggestions } from "@/providers/AutoCompleteMonaco";

export default function QueryTabContent({ tab }) {
  const { theme } = useTheme();
  const { saveTab, updateQueryTab, runQuery, isLoadingQuery } = useTabState();
  const [monacoEditorContent, setMonacoEditorContent] = useState("");
  const [editorInstance, setEditorInstance] = useState(null);

  useEffect(() => {
    setMonacoEditorContent(tab.tab_content);
  }, [tab.tab_content]);

  useEffect(() => {
    const handleRunQueryShortCut = (event) => {
      if (event.metaKey && event.key === "Enter") {
        const query = getSelectedText(editorInstance) || tab.tab_content;
        runQuery(tab.tab_id, query);
      }
    };
    document.addEventListener("keydown", handleRunQueryShortCut);

    const handleSaveQueryShortCut = (event) => {
      if (event.metaKey && event.key === "s") {
        event.preventDefault();
        saveTab(tab);
      }
    };
    document.addEventListener("keydown", handleSaveQueryShortCut);

    return () => {
      document.removeEventListener("keydown", handleRunQueryShortCut);
      document.removeEventListener("keydown", handleSaveQueryShortCut);
    };
  }, [tab, editorInstance]);

  function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return "0 Bytes";

    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ["Bytes", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];

    const i = Math.floor(Math.log(bytes) / Math.log(k));

    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + " " + sizes[i];
  }

  window.sqlCompletionProviderRegistered =
    window.sqlCompletionProviderRegistered || false;

  const handleEditorDidMount = (editor, monaco) => {
    setEditorInstance(editor);

    editor.addAction({
      id: "format-sql",
      label: "Format SQL",
      contextMenuOrder: 1.5,
      run: function (ed) {
        ed.getAction("editor.action.formatDocument").run();
      },
    });

    monaco.languages.registerDocumentFormattingEditProvider("sql", {
      provideDocumentFormattingEdits: function (model) {
        const formattedSql = format(model.getValue());
        return [
          {
            range: model.getFullModelRange(),
            text: formattedSql,
          },
        ];
      },
    });

    if (!window.sqlCompletionProviderRegistered) {
      monaco.languages.registerCompletionItemProvider("sql", {
        provideCompletionItems: (model, position) => {
          const word = model.getWordUntilPosition(position);
          const range = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: word.startColumn,
            endColumn: word.endColumn,
          };

          return {
            suggestions: [...suggestions].map((suggestion) => ({
              ...suggestion,
              range,
            })),
          };
        },
      });
      window.sqlCompletionProviderRegistered = true;
    }

    editor.addAction({
      id: "run-query",
      label: "Run Query",
      keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
      contextMenuGroupId: "navigation",
      contextMenuOrder: 1.5,
      run: function (ed) {
        const query = getSelectedText(ed) || ed.getValue();
        runQuery(tab.tab_id, query);
      },
    });

    editor.onDidChangeModelContent(() => {
      const tab_content = editor.getValue();
      updateQueryTab(tab.tab_id, { tab_content });
    });
  };

  const getSelectedText = (editor) => {
    if (!editor) return "";
    const selection = editor.getSelection();
    return editor.getModel().getValueInRange(selection);
  };

  return (
    <>
      <div className="flex flex-col border p-4 rounded-md">
        <div className="flex items-center justify-between w-full">
          <div className="flex items-center">
            <Input
              value={tab.tab_title}
              className="w-46 h-8 text-xs"
              onChange={(e) => {
                updateQueryTab(tab.tab_id, { tab_title: e.target.value });
              }}
            />
            <Button
              variant="outline"
              onClick={() => {
                saveTab(tab);
              }}
              className="ml-2 h-8 text-primary border-secondary hover:bg-primary hover:text-white"
            >
              Save
            </Button>
          </div>
          <div className="flex items-center">
            <Button
              variant=""
              onClick={() => {
                const query = getSelectedText(editorInstance) || tab.tab_content;
                runQuery(tab.tab_id, query);
              }}
              className="h-8 hover:bg-secondary hover:text-white"
              disabled={isLoadingQuery}
            >
              {isLoadingQuery ? "Running..." : "Run query"}
            </Button>
          </div>
        </div>
        <ResizablePanelGroup
          className="flex mt-4 min-h-[85vh]"
          direction="vertical"
        >
          <ResizablePanel>
            <Editor
              className="w-full h-full"
              language="sql"
              value={tab.tab_content || monacoEditorContent || ""}
              theme={theme === "dark" ? "vs-dark" : "vs-light"}
              onMount={handleEditorDidMount}
              options={{
                minimap: { enabled: false },
                wordWrap: "on",
                fontSize: 14,
                lineNumbers: "on",
                padding: { top: 15, bottom: 10 },
                acceptSuggestionOnCommitCharacter: true,
                acceptSuggestionOnEnter: "on",
                accessibilitySupport: "auto",
                autoIndent: false,
                automaticLayout: true,
                codeLens: true,
                colorDecorators: true,
                contextmenu: true,
                cursorBlinking: "blink",
              }}
            />
          </ResizablePanel>
          <ResizableHandle className="mx-auto m-4" withHandle={true} />
          <ResizablePanel className="flex flex-grow">
            {!tab.tab_results && !tab.tab_errors ? (
              <div className="w-full flex flex-col items-center justify-center">
                <p className="text-lg">No results to display</p>
              </div>
            ) : tab.tab_errors ? (
              <div className="w-full flex flex-col items-center mt-2">
                <p className="text-sm text-destructive">
                  Error: {tab.tab_errors}
                </p>
              </div>
            ) : (
              <div className="w-full p-2 overflow-auto flex flex-col">
                <div
                  className={`${theme === "dark"
                    ? "ag-theme-alpine-dark"
                    : "ag-theme-alpine"
                    }  w-full flex-grow`}
                >
                  {isLoadingQuery ? (
                    <div className="flex justify-center items-center h-full">
                      <Loader2 size={46} className="animate-spin" />
                    </div>
                  ) : (
                    <AgGridReact
                      alwaysShowVerticalScroll={true}
                      rowData={tab.tab_results}
                      enableCellTextSelection={true}
                      columnDefs={
                        tab.tab_results && tab.tab_results.length > 0
                          ? Object.keys(tab.tab_results[0]).map((key) => ({
                            headerName: key,
                            field: key,
                            filter: true,
                          }))
                          : []
                      }
                      defaultColDef={{ resizable: true }}
                      pagination={true}
                      paginationPageSize={20}
                      overlayLoadingTemplate={
                        '<span class="ag-overlay-loading-center">Please wait while your rows are loading</span>'
                      }
                      overlayNoRowsTemplate={`<span class="">This query returned no results</span>`}
                    />
                  )}
                </div>

                <div className="w-full flex items-center justify-between p-2 text-sm font-thin mt-2">
                  <p className="flex items-center">
                    Rows Read:
                    {isLoadingQuery ? (
                      <Loader2 size={16} className="animate-spin" />
                    ) : (
                      tab.tab_results_statistics.rows_read
                    )}
                  </p>
                  <p className="flex items-center">
                    Elapsed:
                    {isLoadingQuery ? (
                      <Loader2 size={16} className="animate-spin" />
                    ) : (
                      tab.tab_results_statistics.elapsed
                    )}
                  </p>
                  <p className="flex items-center">
                    Data Read:
                    {isLoadingQuery ? (
                      <Loader2 size={16} className="animate-spin" />
                    ) : (
                      formatBytes(tab.tab_results_statistics.bytes_read)
                    )}
                  </p>
                  <p className="flex items-center text-sm">
                    Last Run:
                    {isLoadingQuery ? (
                      <Loader2 size={16} className="animate-spin" />
                    ) : (
                      `${new Date(tab.last_run).toUTCString()} (${Math.round(
                        (new Date() - new Date(tab.last_run)) / 60000
                      )} minutes ago)`
                    )}
                  </p>
                  <Popover>
                    <PopoverTrigger>
                      <DownloadIcon className="cursor-pointer" size={20} />
                    </PopoverTrigger>
                    <PopoverContent className="w-max p-2 flex flex-col">
                      <Button
                        variant="link"
                        onClick={() => {
                          downloadCsv(tab.tab_results, tab.tab_title, "csv");
                        }}
                      >
                        <FileSpreadsheetIcon className="mr-2" size={20} />
                        Download CSV
                      </Button>
                      <Button
                        variant="link"
                        onClick={() => {
                          downloadCsv(tab.tab_results, tab.tab_title, "json");
                        }}
                      >
                        <FileJson className="mr-2" size={20} />
                        Download JSON
                      </Button>
                    </PopoverContent>
                  </Popover>
                </div>
              </div>
            )}
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
    </>
  );
}
