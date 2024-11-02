// components/UserTable/TableSkeletons.tsx
import React from "react";
import { Skeleton } from "@/components/ui/skeleton";

const TableSkeletons: React.FC = () => (
  <div className="space-y-4 w-full h-[calc(100vh-300px)]">
    <Skeleton className="h-[calc(15vh-30px)] w-full" />
    <Skeleton className="h-[calc(15vh-30px)] w-full" />
    <Skeleton className="h-[calc(15vh-30px)] w-full" />
    <Skeleton className="h-[calc(15vh-30px)] w-full" />
  </div>
);

export default TableSkeletons;
