import React from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import CHUITable from "@/components/CHUITable";

interface TableInfoTabsProps {
  columns: any[];
  partitions: any[];
  dataSample: any[];
  tableData: {
    create_table_query: string;
    partition_key?: string;
    sorting_key?: string;
    primary_key?: string;
  };
}

const TableInfoTabs: React.FC<TableInfoTabsProps> = ({
  columns,
  partitions,
  dataSample,
  tableData,
}) => {
  return (
    <Tabs defaultValue="columns" className="w-full">
      <TabsList>
        <TabsTrigger value="columns">Columns</TabsTrigger>
        <TabsTrigger value="details">Details</TabsTrigger>
        <TabsTrigger value="partitions">Partitions</TabsTrigger>
        <TabsTrigger value="sample">Data Sample</TabsTrigger>
      </TabsList>

      <TabsContent value="columns">
        <CHUITable
          result={{
            meta: [
              { name: "name", type: "String" },
              { name: "type", type: "String" },
              { name: "default", type: "String" },
              { name: "comment", type: "String" },
            ],
            data: columns.map((column: any) => ({
              ...column,
              default: column.default_kind
                ? `${column.default_kind}: ${column.default_expression}`
                : "N/A",
            })),
            statistics: {
              elapsed: 0,
              rows_read: columns.length,
              bytes_read: 0,
            },
          }}
        />
      </TabsContent>

      <TabsContent value="details">
        <CHUITable
          result={{
            meta: [
              { name: "create_table_query", type: "String" },
              { name: "partition_key", type: "String" },
              { name: "sorting_key", type: "String" },
              { name: "primary_key", type: "String" },
            ],
            data: [
              {
                create_table_query: tableData.create_table_query,
                partition_key: tableData.partition_key || "N/A",
                sorting_key: tableData.sorting_key || "N/A",
                primary_key: tableData.primary_key || "N/A",
              },
            ],
            statistics: {
              elapsed: 0,
              rows_read: 1,
              bytes_read: 0,
            },
          }}
        />
      </TabsContent>

      <TabsContent value="partitions">
        <CHUITable
          result={{
            meta: [
              { name: "partition", type: "String" },
              { name: "rows", type: "UInt64" },
              { name: "bytes", type: "UInt64" },
              { name: "min_time", type: "DateTime" },
              { name: "max_time", type: "DateTime" },
            ],
            data: partitions,
            statistics: {
              elapsed: 0,
              rows_read: partitions.length,
              bytes_read: 0,
            },
          }}
        />
      </TabsContent>

      <TabsContent value="sample">
        <CHUITable
          result={{
            meta: dataSample[0]
              ? Object.keys(dataSample[0]).map((key) => ({
                  name: key,
                  type: "String",
                }))
              : [],
            data: dataSample,
            statistics: {
              elapsed: 0,
              rows_read: dataSample.length,
              bytes_read: 0,
            },
          }}
        />
      </TabsContent>
    </Tabs>
  );
};

export default TableInfoTabs;
