import React from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import CHUITable from "@/components/CHUITable";

interface DataTableTabsProps {
  tablesInDatabase: any[];
  viewsInDatabase: any[];
  topColumns: any[];
}

const DataTableTabs: React.FC<DataTableTabsProps> = ({
  tablesInDatabase,
  viewsInDatabase,
  topColumns,
}) => {
  return (
    <Tabs defaultValue="tables" className="w-full">
      <TabsList>
        <TabsTrigger value="tables">Tables</TabsTrigger>
        <TabsTrigger value="views">Views</TabsTrigger>
        <TabsTrigger value="columns">Top Columns</TabsTrigger>
      </TabsList>

      <TabsContent value="tables">
        <CHUITable
          result={{
            meta: [
              { name: "name", type: "String" },
              { name: "engine", type: "String" },
              { name: "total_rows", type: "UInt64" },
              { name: "total_bytes", type: "UInt64" },
            ],
            data: tablesInDatabase,
            statistics: {
              elapsed: tablesInDatabase.length * 0.1,
              rows_read: tablesInDatabase.length,
              bytes_read: JSON.stringify(tablesInDatabase).length,
            },
          }}
        />
      </TabsContent>

      <TabsContent value="views">
        <CHUITable
          result={{
            meta: [
              { name: "name", type: "String" },
              { name: "engine", type: "String" },
            ],
            data: viewsInDatabase,
            statistics: { elapsed: 0, rows_read: 0, bytes_read: 0 },
          }}
        />
      </TabsContent>

      <TabsContent value="columns">
        <CHUITable
          result={{
            meta: [
              { name: "table", type: "String" },
              { name: "column_name", type: "String" },
              { name: "data_type", type: "String" },
              { name: "columns_in_table", type: "UInt32" },
            ],
            data: topColumns,
            statistics: {
              elapsed: 0,
              rows_read: 0,
              bytes_read: 0,
            },
          }}
        />
      </TabsContent>
    </Tabs>
  );
};

export default DataTableTabs;
