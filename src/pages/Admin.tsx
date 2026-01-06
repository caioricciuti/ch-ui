import { useState } from "react";
import { Button } from "@/components/ui/button";
import UserTable from "@/features/admin/components/UserManagement/index";
import { InfoIcon, ShieldCheck, Users, Database, Settings } from "lucide-react";
import InfoDialog from "@/components/common/InfoDialog";
import ActivateSavedQueries from "@/features/admin/components/ActivateSavedQueries";
import ClickhouseDefaultConfiguration from "@/features/admin/components/ClickhouseDefaultConfiguration";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { GlassCard, GlassCardContent } from "@/components/ui/glass-card";
import { motion } from "framer-motion";

export default function Admin() {
  const [isInfoOpen, setIsInfoOpen] = useState(false);

  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      className="h-full w-full overflow-y-auto"
    >
      <div className="container mx-auto p-6 space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold tracking-tight text-white/90 flex items-center gap-3">
              <ShieldCheck className="w-8 h-8 text-purple-400" />
              Administration
              <span
                onClick={() => setIsInfoOpen(true)}
                className="text-xs bg-purple-500/20 border border-purple-500/50 text-purple-300 px-2 py-0.5 rounded-full cursor-pointer flex items-center gap-1 hover:bg-purple-500/30 transition-colors"
              >
                ALPHA <InfoIcon className="w-3 h-3" />
              </span>
            </h1>
            <p className="text-gray-400 mt-2">Manage users, access controls, and system configurations.</p>
          </div>
        </div>

        <Tabs defaultValue="users" className="space-y-6">
          <TabsList className="bg-white/5 border border-white/10 p-1">
            <TabsTrigger value="users" className="data-[state=active]:bg-purple-500/20 data-[state=active]:text-purple-300">
              <Users className="w-4 h-4 mr-2" /> Users & Roles
            </TabsTrigger>
            <TabsTrigger value="queries" className="data-[state=active]:bg-blue-500/20 data-[state=active]:text-blue-300">
              <Database className="w-4 h-4 mr-2" /> Saved Queries
            </TabsTrigger>
            <TabsTrigger value="config" className="data-[state=active]:bg-orange-500/20 data-[state=active]:text-orange-300">
              <Settings className="w-4 h-4 mr-2" /> Configuration
            </TabsTrigger>
          </TabsList>

          <TabsContent value="users">
            <GlassCard>
              <GlassCardContent className="p-0">
                <UserTable />
              </GlassCardContent>
            </GlassCard>
          </TabsContent>

          <TabsContent value="queries">
            <GlassCard>
              <GlassCardContent className="p-6">
                <h2 className="text-xl font-semibold text-white mb-2">Saved Queries Management</h2>
                <p className="text-gray-400 mb-6">Enable or disable the saved queries feature for this ClickHouse cluster.</p>
                <ActivateSavedQueries />
              </GlassCardContent>
            </GlassCard>
          </TabsContent>

          <TabsContent value="config">
            <GlassCard>
              <GlassCardContent className="p-6">
                <h2 className="text-xl font-semibold text-white mb-2">System Configuration</h2>
                <p className="text-gray-400 mb-6">View and adjust ClickHouse server settings.</p>
                <ClickhouseDefaultConfiguration />
              </GlassCardContent>
            </GlassCard>
          </TabsContent>
        </Tabs>

        <InfoDialog
          title="Administration Page"
          isOpen={isInfoOpen}
          onClose={() => setIsInfoOpen(false)}
          variant="info"
        >
          <div className="flex flex-col gap-2">
            <ul className="list-disc list-inside space-y-1">
              <li>Manage users, roles, and settings.</li>
              <li className="text-sm">This page is only accessible to administrators.</li>
              <li>
                Actions run directly on ClickHouse system tables. Some are <strong className="text-red-400">irreversible</strong>.
              </li>
            </ul>
            <p className="text-xs font-bold mt-2">
              <span className="text-purple-400">ALPHA FEATURE</span> - Subject to change.
            </p>
          </div>
        </InfoDialog>
      </div>
    </motion.div>
  );
}
