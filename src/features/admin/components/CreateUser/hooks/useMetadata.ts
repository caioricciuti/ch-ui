// hooks/useMetadata.ts
import { useState, useEffect } from "react";
import useAppStore from "@/store";
import { toast } from "sonner";

interface Metadata {
  roles: string[];
  databases: string[];
  profiles: string[];
}

const useMetadata = (isOpen: boolean): Metadata => {
  const { runQuery } = useAppStore();
  const [metadata, setMetadata] = useState<Metadata>({
    roles: [],
    databases: [],
    profiles: [],
  });

  useEffect(() => {
    const fetchMetadata = async () => {
      try {
        const rolesResult = await runQuery("SHOW ROLES");
        const roles = !rolesResult.error && rolesResult.data
          ? rolesResult.data.map((row: any) => row.name)
          : [];
        
        const dbResult = await runQuery("SHOW DATABASES");
        const databases = !dbResult.error && dbResult.data
          ? dbResult.data.map((row: any) => row.name)
          : [];

        const profilesResult = await runQuery("SHOW SETTINGS PROFILES");
        const profiles = !profilesResult.error && profilesResult.data
          ? profilesResult.data.map((row: any) => row.name)
          : [];

        setMetadata({ roles, databases, profiles });
      } catch (err) {
        console.error("Failed to fetch metadata:", err);
        toast.error("Failed to fetch metadata.");
      }
    };

    if (isOpen) {
      fetchMetadata();
    }
  }, [isOpen, runQuery]);

  return metadata;
};

export default useMetadata;
