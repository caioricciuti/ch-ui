// components/CreateTable/GeneratedSQL.tsx
import React from "react";
import { Button } from "@/components/ui/button";
import { CopyIcon, CopyCheck } from "lucide-react";

interface GeneratedSQLProps {
  sql: string;
  onCopySQL: () => void;
  statementCopiedToClipBoard: boolean;
}

const GeneratedSQL: React.FC<GeneratedSQLProps> = ({
  sql,
  onCopySQL,
  statementCopiedToClipBoard,
}) => {
  return (
    <div className="mt-4">
      <div className="flex items-center">
        <h3 className="text-lg font-semibold">Generated SQL:</h3>
        <Button
          size="icon"
          variant="link"
          onClick={onCopySQL}
          className="ml-2"
          aria-label="Copy SQL Statement"
        >
          {!statementCopiedToClipBoard ? (
            <CopyIcon height={18} />
          ) : (
            <CopyCheck height={18} className="text-green-400" />
          )}
        </Button>
      </div>
      <pre className="bg-primary/20 p-2 rounded mt-2 text-sm overflow-x-auto">
        {sql}
      </pre>
    </div>
  );
};

export default GeneratedSQL;
