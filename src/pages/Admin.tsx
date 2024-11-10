import { useState } from "react";
import { Button } from "@/components/ui/button";
import UserTable from "@/features/admin/components/UserManagement/index";
import { InfoIcon, ShieldCheck } from "lucide-react";
import InfoDialog from "@/components/common/InfoDialog";
import ActivateSavedQueries from "@/features/admin/components/ActivateSavedQueries";
import ClickhouseDefaultConfiguration from "@/features/admin/components/ClickhouseDefaultConfiguration";

export default function Admin() {
  const [isInfoOpen, setIsInfoOpen] = useState(false);
  const [activeSection, setActiveSection] = useState("config");

  return (
    <div className="max-h-screen w-full overflow-y-auto">
      <div className="container mx-auto p-6">
        <div className="mb-8">
          <h1 className="text-3xl font-medium  mb-2 flex items-center gap-2">
            <ShieldCheck className="w-6 h-6" />
            Administration
            <span
              onClick={() => setIsInfoOpen(true)}
              className="text-xs bg-purple-500/40 border border-purple-500 text-purple-600 px-2 py-1 rounded-md cursor-pointer flex items-center gap-1"
            >
              ALPHA <InfoIcon className="w-4 h-4" />
            </span>
          </h1>
        </div>

        <div className="flex gap-6">
          {/* Sidebar */}
          <div className="w-64">
            <nav className="space-y-2">
              <Button
                variant="ghost"
                className={`w-full justify-start ${
                  activeSection === "users" ? "" : "text-gray-400"
                } hover:bg-muted/50`}
                onClick={() => setActiveSection("users")}
              >
                Users & Roles
              </Button>
              <Button
                variant="ghost"
                className={`w-full justify-start ${
                  activeSection === "queries" ? "" : "text-gray-400"
                } hover:bg-muted/50`}
                onClick={() => setActiveSection("queries")}
              >
                Saved Queries
              </Button>
              <Button
                variant="ghost"
                className={`w-full justify-start ${
                  activeSection === "config" ? "" : "text-gray-400"
                } hover:bg-muted/50`}
                onClick={() => setActiveSection("config")}
              >
                Configuration
              </Button>
            </nav>
          </div>

          {/* Main Content */}
          <div className="flex-1">
            <div className="space-y-6">
              {activeSection === "users" && (
                <div>
                  <UserTable />
                </div>
              )}

              {activeSection === "queries" && (
                <div>
                  <h2 className="text-2xl font-medium mb-2">Saved Queries</h2>
                  <p className="text-gray-400 mb-6">
                    Manage and activate saved queries.
                  </p>
                  <ActivateSavedQueries />
                </div>
              )}

              {activeSection === "config" && (
                <div>
                  <h2 className="text-2xl font-medium  mb-2">Configuration</h2>
                  <p className="text-gray-400 mb-6">
                    Manage ClickHouse configuration settings.
                  </p>
                  <ClickhouseDefaultConfiguration />
                </div>
              )}
            </div>
          </div>
        </div>

        <InfoDialog
          title="Administration Page"
          isOpen={isInfoOpen}
          onClose={() => setIsInfoOpen(false)}
          variant="info"
        >
          <div className="flex flex-col gap-2">
            <ul className="list-disc list-inside">
              <li>Here you can manage users, roles, and settings.</li>
              <li className="text-sm">
                This page is only accessible to administrators.
              </li>
              <li>
                All the actions you take here run queries directly on your
                ClickHouse system database, be aware that those can be{" "}
                <strong className="text-red-500">irreversible.</strong>
              </li>
            </ul>
            <p className="text-xs font-bold">
              This is an <span className="text-purple-500">ALPHA</span> feature
              and is subject to change.
            </p>
          </div>
        </InfoDialog>
      </div>
    </div>
  );
}
