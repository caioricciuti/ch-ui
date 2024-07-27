import React, { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { sql } from "@codemirror/lang-sql";
import { oneDark } from "@codemirror/theme-one-dark";
import { autocompletion } from "@codemirror/autocomplete";

interface SQLEditorProps {
  initialValue?: string;
  onChange?: (value: string) => void;
}

export const SQLEditor: React.FC<SQLEditorProps> = ({
  initialValue = "",
  onChange,
}) => {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);

  useEffect(() => {
    if (editorRef.current && !viewRef.current) {
      const startState = EditorView.updateListener.of((update) => {
        if (update.docChanged && onChange) {
          onChange(update.state.doc.toString());
        }
      });

      viewRef.current = new EditorView({
        doc: initialValue,
        extensions: [
          basicSetup,
          sql(),
          oneDark,
          autocompletion({
            override: [
              (context) => {
                let word = context.matchBefore(/\w+/);
                if (word) {
                  return {
                    from: word.from,
                    options: [
                      { label: "SELECT", type: "keyword" },
                      { label: "FROM", type: "keyword" },
                      { label: "WHERE", type: "keyword" },
                      { label: "GROUP BY", type: "keyword" },
                      { label: "ORDER BY", type: "keyword" },
                      // Add more SQL keywords and table/column names as needed
                    ],
                  };
                }
                return null;
              },
            ],
          }),
          startState,
        ],
        parent: editorRef.current,
      });
    }

    return () => {
      if (viewRef.current) {
        viewRef.current.destroy();
        viewRef.current = null;
      }
    };
  }, [initialValue, onChange]);

  return <div ref={editorRef} className="" />;
};

export default SQLEditor;
