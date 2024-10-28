import { useState } from "react";

import UserTable from "@/components/admin/actions/UserTable";
import { InfoIcon, ShieldCheck } from "lucide-react";
import InfoDialog from "@/components/misc/InfoDialog";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardDescription,
} from "@/components/ui/card";
export default function Admin() {
  const [isInfoOpen, setIsInfoOpen] = useState(false);

  return (
    <div className="flex flex-col gap-4 p-4 container">
      <h1 className="text-2xl font-bold flex items-center gap-2">
        <InfoDialog
          title="Administration Page"
          children={
            <>
              <div className="flex flex-col gap-2">
                <ul className="list-disc list-inside">
                  <li>Here you can manage users, roles, and settings.</li>
                  <li className="text-sm">
                    This page is only accessible to administrators.
                  </li>
                  <li>
                    {" "}
                    All the actions you take here run queries directly on your
                    ClickHouse system database, be aweare that those can be{" "}
                    <strong className="text-red-500">irreversible.</strong>
                  </li>
                </ul>
                <p className="text-xs font-bold">
                  This is an <span className="text-purple-500">ALPHA</span>{" "}
                  feature and is subject to change.
                </p>
              </div>
            </>
          }
          variant="info"
          isOpen={isInfoOpen}
          onClose={() => setIsInfoOpen(false)}
        />
        <span
          onClick={() => setIsInfoOpen(true)}
          className="text-xs bg-purple-500/40 border border-purple-500 text-purple-600 px-2 py-1 rounded-md cursor-pointer flex items-center gap-1"
        >
          ALPHA <InfoIcon className="w-4 h-4" />
        </span>
        <ShieldCheck className="w-6 h-6" /> Administration{" "}
      </h1>
      <div className="flex flex-col gap-4">
        <UserTable />
      </div>
    </div>
  );
}
