import React from "react";

interface TableInfoProps {
  tableData: {
    name: string;
    database: string;
    engine: string;
    total_rows: number;
    total_bytes: number;
    metadata_modification_time: number;
  };
  formatBytes: (bytes: number) => string;
  formatDate: (timestamp: number) => string;
}

const TableInfo: React.FC<TableInfoProps> = ({ tableData, formatBytes }) => {
  return (
    <>
      <div>
        <h2 className="text-2xl font-bold">{tableData.name}</h2>
        <div className="text-xs italic text-muted-foreground mb-4">
          on: {tableData.database}
        </div>
      </div>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4 mb-6">
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Engine</h3>
          <div className="text-2xl font-bold">{tableData.engine}</div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Rows</h3>
          <div className="text-2xl font-bold">
            {tableData.total_rows?.toLocaleString() || "N/A"}
          </div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Size</h3>
          <div className="text-2xl font-bold">
            {tableData.total_bytes ? formatBytes(tableData.total_bytes) : "N/A"}
          </div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Created At</h3>
          <div className="text-2xl font-bold">
            {tableData.metadata_modification_time
              ? (tableData.metadata_modification_time)
              : "N/A"}
          </div>
        </div>
      </div>
    </>
  );
};

export default TableInfo;
