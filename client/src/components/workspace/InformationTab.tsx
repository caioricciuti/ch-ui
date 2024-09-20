import React, { useEffect, useState } from "react";
import { toast } from "sonner";
import api from "@/api/axios.config";
import DatabaseInfo from "@/components/workspace/informationTab/DatabaseInfo";
import DatabaseInfoTabs from "@/components/workspace/informationTab/DatabaseInfoTabs";
import TableInfo from "@/components/workspace/informationTab/TableInfo";
import TableInfoTabs from "@/components/workspace/informationTab/TableInfoTabs";

import { Skeleton } from "@/components/ui/skeleton";

interface InformationTabProps {
  database: string;
  table?: string;
}

const InformationTab: React.FC<InformationTabProps> = ({ database, table }) => {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [data, setData] = useState<any>({});

  useEffect(() => {
    const fetchInformation = async () => {
      try {
        setLoading(true);
        setError(null);

        const response = await api.get(
          `ch-queries/metrics?database=${database}${
            table ? `&table=${table}` : ""
          }`
        );

        if (!response.data) {
          throw new Error(`Failed to fetch data: ${response.statusText}`);
        }
        const result = await response.data;
        setData(result);
      } catch (err: any) {
        setError("Failed to load data.");
        toast.error(`Failed to load data: ${err.message || err}`);
      } finally {
        setLoading(false);
      }
    };

    fetchInformation();
  }, [database, table]);

  const formatBytes = (bytes: number) => {
    console.log(bytes);
    if (bytes == 0 || bytes === undefined || Number.isNaN(bytes))
      return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  const formatDate = (timestamp: number) => {
    return new Date(timestamp * 1000).toLocaleString();
  };

  if (loading) {
    return (
      <div className="p-4 space-y-4">
        <Skeleton className="h-8 w-3/4" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error) {
    return <div className="p-4 text-red-500">{error}</div>;
  }

  const {
    databaseInfo = [],
    tablesInDatabase = [],
    viewsInDatabase = [],
    topColumns = [],
    tableInfo = [],
    columns = [],
    partitions = [],
    dataSample = [],
  } = data;

  const dbInfo = databaseInfo[0] || {};
  const tableData = tableInfo[0] || {};

  return (
    <div className="h-full w-full overflow-auto p-6">
      {!table ? (
        <>
          <DatabaseInfo
            database={database}
            dbInfo={dbInfo}
            formatBytes={formatBytes}
          />
          <DatabaseInfoTabs
            tablesInDatabase={tablesInDatabase}
            viewsInDatabase={viewsInDatabase}
            topColumns={topColumns}
          />
        </>
      ) : (
        <>
          <TableInfo
            tableData={tableData}
            formatBytes={formatBytes}
            formatDate={formatDate}
          />
          <TableInfoTabs
            columns={columns}
            partitions={partitions}
            dataSample={dataSample}
            tableData={tableData}
          />
        </>
      )}
    </div>
  );
};

export default InformationTab;
