import { Skeleton } from "@/components/ui/skeleton";

export const TableSkeletons = () => (
  <div className="space-y-4 w-full h-[calc(100vh-300px)]">
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
    </div>
  );