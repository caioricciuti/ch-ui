//monacoConfig.ts
// This file contains the configuration for the Monaco Editor used in the application.

import * as monaco from "monaco-editor";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import api from "../api/axios.config";
import { format } from "sql-formatter";

let isInitialized = false;

// Define interfaces for the database structure
interface Column {
  name: string;
  type: string;
}

interface Table {
  name: string;
  type: string;
  children: Column[];
}

interface Database {
  name: string;
  type: string;
  children: Table[];
}

// Cache for database structure
let dbStructureCache: Database[] | null = null;

// cache for functions
let functionsCache: string[] | null = null;

// cache for keywords
let keywordsCache: string[] | null = null;

// Setting up the Monaco Environment to use the editor worker
self.MonacoEnvironment = {
  getWorker() {
    return new editorWorker();
  },
};

// Ensure the Monaco Environment is initialized
export function ensureMonacoEnvironment() {
  if (typeof self.MonacoEnvironment === "undefined") {
    self.MonacoEnvironment = {
      getWorker: () => new editorWorker(),
    };
  }
}

// Fetch database, tables, and columns for ClickHouse from the API
async function getDatabasesTablesAndColumns(): Promise<Database[]> {
  if (dbStructureCache) {
    return dbStructureCache;
  }

  try {
    const response = await api.get("/ch-queries/intellisense");
    dbStructureCache = response.data as Database[];
    return dbStructureCache;
  } catch (err) {
    console.error("Error fetching database data:", err);
    return [];
  }
}

// async function get functions from the API
async function getFunctions(): Promise<string[]> {
  if (functionsCache) {
    return functionsCache;
  }

  try {
    const response = await api.get("/ch-queries/functions");
    functionsCache = response.data as string[];
    return response.data as string[];
  } catch (err) {
    console.error("Error fetching functions data:", err);
    return [];
  }
}

// async function get keywords from the API
async function getKeywords(): Promise<string[]> {
  if (keywordsCache) {
    return keywordsCache;
  }

  try {
    const response = await api.get("/ch-queries/keywords");
    keywordsCache = response.data as string[];
    return response.data as string[];
  } catch (err) {
    console.error("Error fetching keywords data:", err);
    return [];
  }
}

// Parse the SQL query to determine the context
function parseQueryContext(
  query: string,
  position: monaco.Position
): { database?: string; table?: string; isTypingDatabase: boolean } {
  const lines = query.split("\n");
  const currentLine = lines[position.lineNumber - 1].substring(
    0,
    position.column
  );
  const tokens = currentLine.split(/\s+/);

  let database: string | undefined;
  let table: string | undefined;
  let isTypingDatabase = false;

  for (let i = tokens.length - 1; i >= 0; i--) {
    const token = tokens[i];
    if (token.includes(".")) {
      const parts = token.split(".");
      if (parts.length === 2 && parts[1] === "") {
        database = parts[0];
        isTypingDatabase = true;
        break;
      } else if (parts.length === 2) {
        [database, table] = parts;
      } else if (parts.length === 3) {
        [database, table] = parts.slice(0, 2);
      }
      break;
    }
    if (token.toLowerCase() === "from" && i + 1 < tokens.length) {
      table = tokens[i + 1];
      break;
    }
  }

  return { database, table, isTypingDatabase };
}

// Initialize Monaco editor with ClickHouse SQL language features
export const initializeMonacoGlobally = async () => {
  if (isInitialized) return;

  ensureMonacoEnvironment();

  // Register the SQL language
  monaco.languages.register({ id: "sql" });

  // Set language configuration for SQL
  monaco.languages.setLanguageConfiguration("sql", {
    brackets: [
      ["(", ")"],
      ["[", "]"],
    ],
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: "[", close: "]" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
  });

  // Set monarch tokens provider for SQL syntax highlighting
  monaco.languages.setMonarchTokensProvider("sql", {
    keywords: [
      "SELECT",
      "FROM",
      "WHERE",
      "ORDER BY",
      "GROUP BY",
      "LIMIT",
      "JOIN",
      "INSERT",
      "UPDATE",
      "DELETE",
      "CREATE",
      "ALTER",
      "DROP",
      "TABLE",
      "INDEX",
      "VIEW",
      "TRIGGER",
      "PROCEDURE",
      "FUNCTION",
      "DATABASE",
    ],
    operators: [
      "=",
      ">",
      "<",
      "<=",
      ">=",
      "<>",
      "!=",
      "AND",
      "OR",
      "NOT",
      "LIKE",
      "IN",
      "BETWEEN",
    ],
    tokenizer: {
      root: [
        [
          /[a-zA-Z_]\w*/,
          { cases: { "@keywords": "keyword", "@default": "identifier" } },
        ],
        [/[<>!=]=?/, "operator"],
        [/[0-9]+/, "number"],
        [/"/, { token: "string.quote", bracket: "@open", next: "@string" }],
        [/'/, { token: "string.quote", bracket: "@open", next: "@string2" }],
        [/--.*$/, "comment"],
      ],
      string: [
        [/[^"]+/, "string"],
        [/"/, { token: "string.quote", bracket: "@close", next: "@pop" }],
      ],
      string2: [
        [/[^']+/, "string"],
        [/'/, { token: "string.quote", bracket: "@close", next: "@pop" }],
      ],
      comment: [
        [/[^-]+/, "comment"],
        [/--/, "comment"],
      ],
    },
  });

  // Register completion item provider for SQL
  monaco.languages.registerCompletionItemProvider("sql", {
    provideCompletionItems: async (model, position) => {
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };

      const dbStructure = await getDatabasesTablesAndColumns();
      const queryContext = parseQueryContext(model.getValue(), position);
      const clickHouseFunctionsArray = await getFunctions();
      const clickHouseKeywordsArray = await getKeywords(); // Fetch keywords from API

      const suggestions: monaco.languages.CompletionItem[] = [];

      dbStructure.forEach((database: Database) => {
        if (
          !queryContext.database ||
          database.name
            .toLowerCase()
            .startsWith(queryContext.database.toLowerCase())
        ) {
          if (queryContext.isTypingDatabase || !queryContext.database) {
            suggestions.push({
              label: `${database.name}`,
              kind: monaco.languages.CompletionItemKind.Module,
              insertText: `${database.name}`,
              detail: "Database",
              range: range,
            });
          }

          if (
            queryContext.isTypingDatabase ||
            database.name === queryContext.database
          ) {
            database.children.forEach((table: Table) => {
              if (
                !queryContext.table ||
                table.name
                  .toLowerCase()
                  .startsWith(queryContext.table.toLowerCase())
              ) {
                suggestions.push({
                  label: `${table.name}`,
                  kind: monaco.languages.CompletionItemKind.Struct,
                  insertText: `${table.name}`,
                  detail: `Table in ${database.name}`,
                  range: range,
                });

                if (queryContext.table && table.name === queryContext.table) {
                  table.children.forEach((column: Column) => {
                    suggestions.push({
                      label: `${column.name}`,
                      kind: monaco.languages.CompletionItemKind.Field,
                      insertText: `${column.name}`,
                      detail: `${database.name}.${table.name}.${column.type}`,
                      range: range,
                    });
                  });
                }
              }
            });
          }
        }
      });

      // Add SQL keyword suggestions from fetched keywords
      const keywordSuggestions = clickHouseKeywordsArray.map((keyword) => ({
        label: keyword,
        kind: monaco.languages.CompletionItemKind.Keyword,
        insertText: keyword,
        range: range,
      }));

      // Add ClickHouse functions suggestions
      const chFunctions = clickHouseFunctionsArray.map((chFunc: string) => ({
        label: chFunc,
        kind: monaco.languages.CompletionItemKind.Function,
        insertText: `${chFunc}()`,
        range: range,
      }));

      return {
        suggestions: [...suggestions, ...keywordSuggestions, ...chFunctions],
      };
    },
  });

  // Use sql formatter for formatting SQL code using import { format } from "sql-formatter";
  monaco.languages.registerDocumentFormattingEditProvider("sql", {
    provideDocumentFormattingEdits: (model) => {
      const formatted = format(model.getValue(), { language: "sql" });
      return [
        {
          range: model.getFullModelRange(),
          text: formatted,
        },
      ];
    },
  });

  isInitialized = true;
};

// Create a Monaco Editor instance
export const createMonacoEditor = (
  container: HTMLElement,
  theme: string
): monaco.editor.IStandaloneCodeEditor => {
  const editor = monaco.editor.create(container, {
    language: "sql",
    theme: theme || "vs-dark",
    automaticLayout: true,
    tabSize: 2,
    minimap: { enabled: false },
    padding: { top: 10 },
    suggestOnTriggerCharacters: true,
    quickSuggestions: true,
    wordBasedSuggestions: "off",
  });

  return editor;
};
