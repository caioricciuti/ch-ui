import React from "react";

interface DatabaseInfoProps {
  database: string;
  dbInfo: {
    engine: string;
    tables: number;
    total_rows: number;
    total_bytes: number;
  };
  formatBytes: (bytes: number) => string;
}

const DatabaseInfo: React.FC<DatabaseInfoProps> = ({
  database,
  dbInfo,
  formatBytes,
}) => {
  return (
    <>
      <h2 className="text-2xl font-bold mb-4">Database: {database}</h2>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4 mb-6">
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Engine</h3>
          <div className="text-2xl font-bold">{dbInfo.engine}</div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Tables</h3>
          <div className="text-2xl font-bold">{dbInfo.tables}</div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Rows</h3>
          <div className="text-2xl font-bold">
            {dbInfo.total_rows?.toLocaleString() || "N/A"}
          </div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Size</h3>
          <div className="text-2xl font-bold">
            {dbInfo.total_bytes === null ? "N/A" : formatBytes(dbInfo.total_bytes)}
          </div>
        </div>
      </div>
    </>
  );
};

export default DatabaseInfo;
