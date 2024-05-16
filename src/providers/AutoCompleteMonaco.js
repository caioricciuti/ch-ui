import * as monaco from "monaco-editor";
import { readAllFromDB } from "@/lib/tablesIndexedDB";

const databases = await readAllFromDB();
const databaseNames = databases.map((db) => db.id);

let tableArray = databases.flatMap((db) => db);
let tables = tableArray.map((table) => table.name);

export const suggestions = [
  // Standard SQL keywords
  {
    label: "SELECT",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "SELECT ",
  },
  {
    label: "FROM",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "FROM ",
  },
  {
    label: "WHERE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "WHERE ",
  },
  {
    label: "AND",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "AND ",
  },
  {
    label: "OR",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "OR ",
  },
  {
    label: "ORDER BY",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "ORDER BY ",
  },
  {
    label: "GROUP BY",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "GROUP BY ",
  },
  {
    label: "HAVING",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "HAVING ",
  },
  {
    label: "LIMIT",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "LIMIT ",
  },
  {
    label: "INNER JOIN",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "INNER JOIN ",
  },
  {
    label: "LEFT JOIN",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "LEFT JOIN ",
  },
  {
    label: "RIGHT JOIN",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "RIGHT JOIN ",
  },
  {
    label: "FULL JOIN",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "FULL JOIN ",
  },
  {
    label: "CROSS JOIN",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "CROSS JOIN ",
  },
  {
    label: "ON",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "ON ",
  },

  // ClickHouse-specific keywords and functions
  {
    label: "ARRAY JOIN",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "ARRAY JOIN ",
  },
  {
    label: "TUMBLE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "TUMBLE(",
  },
  {
    label: "HOP",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "HOP(",
  },
  {
    label: "WINDOW",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "WINDOW ",
  },
  {
    label: "SAMPLE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "SAMPLE ",
  },

  // ClickHouse-specific aggregate functions
  {
    label: "countIf",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "countIf(",
  },
  {
    label: "sumIf",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "sumIf(",
  },
  {
    label: "avgIf",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "avgIf(",
  },
  {
    label: "minIf",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "minIf(",
  },
  {
    label: "maxIf",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "maxIf(",
  },

  // Data manipulation and schema modification keywords
  {
    label: "ALTER TABLE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "ALTER TABLE ",
  },
  {
    label: "CREATE TABLE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "CREATE TABLE ",
  },
  {
    label: "DROP TABLE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "DROP TABLE ",
  },
  {
    label: "INSERT INTO",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "INSERT INTO ",
  },
  {
    label: "UPDATE",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "UPDATE ",
  },
  {
    label: "DELETE FROM",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "DELETE FROM ",
  },

  // Functions for data transformation
  {
    label: "toDateTime",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "toDateTime(",
  },
  {
    label: "toDate",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "toDate(",
  },
  {
    label: "toString",
    kind: monaco.languages.CompletionItemKind.Function,
    insertText: "toString(",
  },

  // Logical and comparison operators
  {
    label: "IS NULL",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "IS NULL ",
  },
  {
    label: "IS NOT NULL",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "IS NOT NULL ",
  },
  {
    label: "EXISTS",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "EXISTS ",
  },
  {
    label: "NOT EXISTS",
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: "NOT EXISTS ",
  },
];
// add the database names to the suggestions
databaseNames.forEach((database) => {
  suggestions.push({
    label: database,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: database,
  });
});

tables.forEach((table) => {
  suggestions.push({
    label: table,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: table,
  });
});
