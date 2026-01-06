import { useEffect, useState } from "react";
import useAppStore from "@/store";
import { Loader2, RefreshCcw, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { format } from "date-fns";
import { redactSecrets } from "@/lib/utils";
import { GlassCard, GlassCardContent, GlassCardHeader, GlassCardTitle } from "@/components/ui/glass-card";
import { motion } from "framer-motion";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import DashboardGrid from "@/features/metrics/components/DashboardGrid";
import UPlotMetricItemComponent from "@/features/metrics/components/UPlotMetricItemComponent";
import { exceptionsConfig } from "@/features/metrics/config/exceptionsConfig";
import { TimeRangeProvider } from "@/features/metrics/context/TimeRangeContext";

const LogsPage = () => {
  const { clickHouseClient } = useAppStore();
  const [logs, setLogs] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");

  const fetchLogs = async () => {
    if (!clickHouseClient) return;
    setLoading(true);
    try {
      const resultSet = await clickHouseClient.query({
        query: `
          SELECT
            event_time,
            query_id,
            user,
            query,
            query_duration_ms,
            exception
          FROM system.query_log
          WHERE event_time > now() - INTERVAL 1 HOUR
          ORDER BY event_time DESC
          LIMIT 100
        `,
        format: "JSONEachRow",
      });
      const result = await resultSet.json();
      setLogs(result as any[]);
    } catch (error) {
      console.error("Failed to fetch logs", error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchLogs();
  }, [clickHouseClient]);

  const filteredLogs = logs.filter((log) =>
    JSON.stringify(log).toLowerCase().includes(searchTerm.toLowerCase())
  );

  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      className="container mx-auto p-6 space-y-6"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-white/90">Logs & Analysis</h1>
          <p className="text-gray-400">System activity and error tracking</p>
        </div>
        <Button onClick={fetchLogs} disabled={loading} variant="outline" className="bg-white/5 border-white/10 hover:bg-white/10 text-white">
          <RefreshCcw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      <Tabs defaultValue="query_log" className="w-full">
        <TabsList className="bg-white/5 border border-white/10 p-1">
          <TabsTrigger value="query_log" className="data-[state=active]:bg-purple-500/20 data-[state=active]:text-purple-200">
            Query History
          </TabsTrigger>
          <TabsTrigger value="exceptions" className="data-[state=active]:bg-purple-500/20 data-[state=active]:text-purple-200">
            Exceptions Analysis
          </TabsTrigger>
        </TabsList>

        <TabsContent value="query_log" className="mt-6">
          <GlassCard>
            <GlassCardHeader className="flex flex-row items-center justify-between space-y-0 pb-7">
              <GlassCardTitle>Last 100 Queries</GlassCardTitle>
              <div className="relative w-72">
                <Search className="absolute left-3 top-2.5 h-4 w-4 text-gray-500" />
                <Input
                  placeholder="Search logs..."
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                  className="pl-9 bg-black/20 border-white/10 text-white focus:bg-white/5"
                />
              </div>
            </GlassCardHeader>
            <GlassCardContent>
              <div className="rounded-md border border-white/10 overflow-hidden">
                <Table>
                  <TableHeader className="bg-white/5">
                    <TableRow className="border-white/10 hover:bg-white/5">
                      <TableHead className="text-gray-400">Time</TableHead>
                      <TableHead className="text-gray-400">User</TableHead>
                      <TableHead className="text-gray-400">Duration (ms)</TableHead>
                      <TableHead className="text-gray-400">Query</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filteredLogs.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={4} className="h-24 text-center text-gray-500">
                          No logs found.
                        </TableCell>
                      </TableRow>
                    ) : (
                      filteredLogs.map((log) => (
                        <TableRow key={log.query_id} className="border-white/10 hover:bg-white/5 transition-colors">
                          <TableCell className="text-gray-300 font-mono text-xs">
                            {format(new Date(log.event_time), "HH:mm:ss")}
                          </TableCell>
                          <TableCell className="text-gray-300">{log.user}</TableCell>
                          <TableCell className="text-gray-300">{log.query_duration_ms}</TableCell>
                          <TableCell className="max-w-[400px] truncate font-mono text-xs text-blue-300" title={redactSecrets(log.query)}>
                            {redactSecrets(log.query)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </GlassCardContent>
          </GlassCard>
        </TabsContent>

        <TabsContent value="exceptions" className="mt-6">
          <TimeRangeProvider defaultPreset="1h">
            <DashboardGrid
              scope="exceptions"
              items={exceptionsConfig.items || []}
              renderItem={(item) => (
                <UPlotMetricItemComponent item={item} />
              )}
              allowReset={false}
            />
          </TimeRangeProvider>
        </TabsContent>
      </Tabs>
    </motion.div>
  );
};

export default LogsPage;