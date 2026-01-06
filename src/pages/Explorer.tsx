import { useEffect } from "react";
import { motion } from "framer-motion";
import DatabaseExplorer from "@/features/explorer/components/DataExplorer";
import WorkspaceTabs from "@/features/workspace/components/WorkspaceTabs";
import {
    ResizableHandle,
    ResizablePanel,
    ResizablePanelGroup,
} from "@/components/ui/resizable";
import CreateTable from "@/features/explorer/components/CreateTable";
import CreateDatabase from "@/features/explorer/components/CreateDatabase";
import UploadFromFile from "@/features/explorer/components/UploadFile";

const ExplorerPage = () => {
    useEffect(() => {
        document.title = "ClickHouse UI | Explorer";
    }, []);

    return (
        <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5 }}
            className="h-full w-full flex flex-col p-4 space-y-4"
        >
            <div className="flex items-center justify-between px-2">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight text-white/90">Data Explorer</h1>
                    <p className="text-gray-400 text-sm">Manage databases and run SQL queries</p>
                </div>
            </div>

            {/* Modals */}
            <CreateTable />
            <CreateDatabase />
            <UploadFromFile />

            <div className="flex-1 rounded-xl border border-white/10 bg-black/40 backdrop-blur-xl overflow-hidden shadow-2xl">
                <ResizablePanelGroup direction="horizontal">
                    <ResizablePanel className="overflow-hidden flex flex-col" defaultSize={25} minSize={15}>
                        <DatabaseExplorer />
                    </ResizablePanel>
                    <ResizableHandle withHandle className="bg-white/10 hover:bg-purple-500/50 transition-colors" />
                    <ResizablePanel
                        className="overflow-hidden flex flex-col"
                        defaultSize={75}
                        minSize={40}
                    >
                        <div className="h-full w-full bg-black/20">
                            <WorkspaceTabs />
                        </div>
                    </ResizablePanel>
                </ResizablePanelGroup>
            </div>
        </motion.div>
    );
};

export default ExplorerPage;
