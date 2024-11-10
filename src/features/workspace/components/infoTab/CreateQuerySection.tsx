import React, { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Copy } from "lucide-react";

interface CreateQuerySectionProps {
  data: any;
}

const CreateQuerySection: React.FC<CreateQuerySectionProps> = ({ data }) => {
  const [copied, setCopied] = useState(false);

  const copyToClipboard = async () => {
    await navigator.clipboard.writeText(data.create_table_query);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-lg font-bold">Create Table Query</CardTitle>
        <button
          onClick={copyToClipboard}
          className="flex items-center space-x-2 text-sm text-muted-foreground hover:text-primary transition-colors"
        >
          <Copy className="w-4 h-4" />
          <span>{copied ? "Copied!" : "Copy"}</span>
        </button>
      </CardHeader>
      <CardContent>
        <pre className="bg-muted p-4 rounded-lg overflow-x-auto">
          <code className="text-sm">{data.create_table_query}</code>
        </pre>
      </CardContent>
    </Card>
  );
};

export default CreateQuerySection;
