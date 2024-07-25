import React, { useState, useMemo } from "react";
import {
  ChevronRight,
  ChevronDown,
  Database,
  Table,
  FileSpreadsheet,
  Search,
  SearchX,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";

export type NodeType = "database" | "table" | "view";

export interface TreeNodeData {
  name: string;
  type: NodeType;
  children?: TreeNodeData[];
}

interface TreeNodeProps {
  node: TreeNodeData;
  level: number;
  searchTerm: string;
}

interface DatabaseExplorerProps {
  data: TreeNodeData[];
}

const TreeNode: React.FC<TreeNodeProps> = ({ node, level, searchTerm }) => {
  const [isOpen, setIsOpen] = useState(false);

  const toggleOpen = () => setIsOpen(!isOpen);

  const getIcon = () => {
    if (node.type === "database") return <Database className="w-4 h-4 mr-2" />;
    if (node.type === "table") return <Table className="w-4 h-4 mr-2" />;
    if (node.type === "view")
      return <FileSpreadsheet className="w-4 h-4 mr-2" />;
    return null;
  };

  const matchesSearch = node.name
    .toLowerCase()
    .includes(searchTerm.toLowerCase());
  const childrenMatchSearch = node.children?.some(
    (child) =>
      child.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      child.children?.some((grandchild) =>
        grandchild.name.toLowerCase().includes(searchTerm.toLowerCase())
      )
  );

  if (searchTerm && !matchesSearch && !childrenMatchSearch) {
    return null;
  }

  return (
    <div>
      <div
        className={`flex items-center py-1 px-2 hover:bg-secondary hover:rounded-md cursor-pointer
            ${node.type === "database" ? "mb-1" : "border-b"}
            
            
            ${level > 0 ? "ml-2" : ""} ${matchesSearch ? "" : ""}`}
        onClick={toggleOpen}
      >
        {node.children ? (
          isOpen ? (
            <ChevronDown className="w-4 h-4 mr-1" />
          ) : (
            <ChevronRight className="w-4 h-4 mr-1" />
          )
        ) : (
          <div className="w-4 mr-1" />
        )}
        {getIcon()}
        <span
          className={`
                ${node.type === "database" ? "font-semibold" : "text-sm"}
            `}
        >
          {node.name}
        </span>
      </div>
      {(isOpen || searchTerm) && node.children && (
        <div>
          {node.children.map((child, index) => (
            <TreeNode
              key={index}
              node={child}
              level={level + 1}
              searchTerm={searchTerm}
            />
          ))}
        </div>
      )}
    </div>
  );
};

const DatabaseExplorer: React.FC<DatabaseExplorerProps> = ({ data }) => {
  const [searchTerm, setSearchTerm] = useState("");

  const filteredData = useMemo(() => {
    if (!searchTerm) return data;
    return data.filter(
      (node) =>
        node.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
        node.children?.some(
          (child) =>
            child.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
            child.children?.some((grandchild) =>
              grandchild.name.toLowerCase().includes(searchTerm.toLowerCase())
            )
        )
    );
  }, [data, searchTerm]);

  return (
    <div className="w-64 border-r">
      <div className="p-4 border-b">
        <h2 className="text-lg font-semibold mb-2">Explorer</h2>
        <div className="relative">
          <Search className="w-4 h-4 absolute left-2 top-1/2 transform -translate-y-1/2 text-gray-400" />
          <Input
            type="text"
            placeholder="Type to search"
            value={searchTerm}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              setSearchTerm(e.target.value)
            }
            className="pl-8 pr-4 py-2 w-full"
          />
        </div>
      </div>
      <ScrollArea className="h-[calc(100vh-180px)]">
        <div className="p-2">
          {filteredData && filteredData.length > 0 ? (
            filteredData.map((node, index) => (
              <TreeNode
                key={index}
                node={node}
                level={0}
                searchTerm={searchTerm}
              />
            ))
          ) : (
            <div className="p-4 text-gray-500">
              <SearchX className="w-8 h-8 mx-auto" />
              <p className="text-center mt-2">No results found</p>
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
};

export default DatabaseExplorer;
