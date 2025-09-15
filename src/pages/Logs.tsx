"use client";

import React, { useState, useEffect, useRef } from "react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import useAppStore from "@/store";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  AlertTriangle,
  AlertCircle,
  ClockIcon,
  Download,
  FileText,
  RefreshCcw,
  Search,
  X,
  Copy,
  CheckCircle,
  Eye,
} from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

interface LogEntry {
  id: string;
  timestamp: string;
  type: string;
  component: string;
  message: string;
  details: string;
}

interface LogTableProps {
  logs: LogEntry[];
  isLoading: boolean;
  onLogClick: (log: LogEntry) => void;
}

const LogTable: React.FC<LogTableProps> = ({ logs, isLoading, onLogClick }) => {
  if (isLoading) {
    return (
      <div className="flex flex-col gap-2 p-4 border border-muted rounded-md">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="h-12 bg-muted rounded-md animate-pulse" />
        ))}
      </div>
    );
  }
  if (logs.length === 0) {
    return (
      <div className="text-center py-12">
        <FileText className="mx-auto h-12 w-12 text-muted-foreground/50 mb-4" />
        <h3 className="text-lg font-medium mb-2">No logs found</h3>
        <p className="text-muted-foreground">
          Try adjusting your search filters
        </p>
      </div>
    );
  }
  return (
    <div className="overflow-hidden">
      <Table>
        <TableHeader className="sticky top-0 bg-background z-10">
          <TableRow>
            <TableHead className="w-[180px]">Timestamp</TableHead>
            <TableHead className="w-[100px]">Type</TableHead>
            <TableHead className="w-[150px]">Component</TableHead>
            <TableHead>Message</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {logs.map((log) => (
            <TableRow
              key={log.id}
              className="group cursor-pointer hover:bg-muted/50"
              onClick={() => onLogClick(log)}
            >
              <TableCell className="font-mono text-xs">
                <div className="flex items-center">
                  <ClockIcon className="h-4 w-4 mr-2 text-muted-foreground" />
                  {formatTimestamp(log.timestamp)}
                </div>
              </TableCell>
              <TableCell>
                <LogTypeBadge type={log.type} />
              </TableCell>
              <TableCell className="text-xs truncate max-w-56">{log.component}</TableCell>
              <TableCell className="flex items-center space-x-2 truncate justify-between">
                <div className="font-mono text-sm truncate max-w-[700px]">
                  {log.message}
                </div>
                <div className="text-xs text-muted-foreground mt-1 hidden group-hover:block flex-row items-center space-x-2">
                  <Eye className="h-4 w-4 mr-1" />
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
};

const LogTypeBadge: React.FC<{ type: string }> = ({ type }) => {
  // Map ClickHouse log levels to UI variants
  const variantMap: Record<string, string> = {
    Error: "destructive",
    Warning: "warning",
    Information: "secondary",
    Debug: "outline",
    Trace: "outline",
    Fatal: "destructive",
  };
  
  // Map ClickHouse log levels to display text
  const displayText: Record<string, string> = {
    Error: "ERROR",
    Warning: "WARNING",
    Information: "INFO",
    Debug: "DEBUG",
    Trace: "TRACE",
    Fatal: "FATAL",
  };
  
  return (
    <Badge variant={(variantMap[type] as any) || "default"} className="">
      {displayText[type] || type}
    </Badge>
  );
};

const LogDetailView: React.FC<{ log: LogEntry | null }> = ({ log }) => {
  const [copied, setCopied] = useState(false);
  
  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  
  if (!log) return null;
  
  const logDetails = {
    "Log ID": log.id,
    Timestamp: formatTimestamp(log.timestamp),
    Type: log.type,
    Component: log.component,
    Message: log.message,
    Details: log.details,
  };
  
  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <LogTypeBadge type={log.type} />
        <Button
          variant="outline"
          size="sm"
          onClick={() => copyToClipboard(JSON.stringify(log, null, 2))}
        >
          {copied ? (
            <>
              <CheckCircle className="h-4 w-4 mr-2 text-green-500" />
              Copied
            </>
          ) : (
            <>
              <Copy className="h-4 w-4 mr-2" />
              Copy JSON
            </>
          )}
        </Button>
      </div>
      <div className="space-y-4">
        {Object.entries(logDetails).map(([key, value]) => (
          <div key={key} className="space-y-1">
            <Label className="text-xs text-muted-foreground">{key}</Label>
            <div
              className={`p-2 rounded-md bg-muted ${
                key === "Message" || key === "Details"
                  ? "font-mono text-sm whitespace-pre-wrap break-words"
                  : "font-mono text-sm"
              }`}
            >
              {value || "-"}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

// Helper function to format timestamps
const formatTimestamp = (timestamp: string) => {
  try {
    const date = new Date(timestamp);
    return date.toLocaleString('en-US', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
      hour12: false,
    });
  } catch (e) {
    return timestamp;
  }
};

const LogsPage: React.FC = () => {
  const { runQuery, clickHouseClient, isServerAvailable } = useAppStore();
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [activeTab, setActiveTab] = useState("system");
  const [error, setError] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<LogEntry | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [scrollAreaHeight, setScrollAreaHeight] = useState("500px");
  const [autoRefresh, setAutoRefresh] = useState<number | null>(null);
  
  // Filters
  const [searchTerm, setSearchTerm] = useState("");
  const [logType, setLogType] = useState<string>("all");
  const [timeRange, setTimeRange] = useState<string>("1h"); // Default to 1 hour
  
  // Time range options
  const timeRangeOptions = [
    { value: "1h", label: "Last hour", hours: 1 },
    { value: "6h", label: "Last 6 hours", hours: 6 },
    { value: "12h", label: "Last 12 hours", hours: 12 },
    { value: "24h", label: "Last 24 hours", hours: 24 },
    { value: "3d", label: "Last 3 days", hours: 72 },
    { value: "7d", label: "Last 7 days", hours: 168 },
    { value: "14d", label: "Last 14 days", hours: 336 },
  ];
  
  // Auto refresh options
  const refreshOptions = [
    { value: null, label: "Manual" },
    { value: 5000, label: "5 seconds" },
    { value: 15000, label: "15 seconds" },
    { value: 30000, label: "30 seconds" },
    { value: 60000, label: "1 minute" },
  ];
  
  // Set up auto-refresh interval
  useEffect(() => {
    if (!autoRefresh) return undefined;
    
    const intervalId = setInterval(() => {
      fetchLogs();
    }, autoRefresh);
    
    return () => clearInterval(intervalId);
  }, [autoRefresh, timeRange, logType, searchTerm, activeTab]);
  
  // Calculate from and to dates based on selected time range
  const getTimeRangeDates = (): { fromDate: string, toDate: string } => {
    const now = new Date();
    const hoursToSubtract = timeRangeOptions.find(option => option.value === timeRange)?.hours || 1;
    
    const fromDate = new Date(now);
    fromDate.setHours(fromDate.getHours() - hoursToSubtract);
    
    return {
      fromDate: fromDate.toISOString(),
      toDate: now.toISOString()
    };
  };
  
  // Calculate the available height for the scroll area
  useEffect(() => {
    const calculateHeight = () => {
      if (scrollContainerRef.current) {
        const rect = scrollContainerRef.current.getBoundingClientRect();
        const windowHeight = window.innerHeight;
        const availableHeight = windowHeight - rect.top - 150; // 20px bottom margin
        setScrollAreaHeight(`${Math.max(300, availableHeight)}px`);
      }
    };
    calculateHeight();
    window.addEventListener("resize", calculateHeight);
    return () => {
      window.removeEventListener("resize", calculateHeight);
    };
  }, []);
  
  // Function to handle log click
  const handleLogClick = (log: LogEntry) => {
    setSelectedLog(log);
    setSheetOpen(true);
  };
  
  // Function to fetch logs
  const fetchLogs = async () => {
    if (!isServerAvailable || !clickHouseClient) {
      setError("Not connected to ClickHouse. Please connect first.");
      setIsLoading(false);
      return;
    }
    
    setIsLoading(true);
    setError(null);
    
    try {
      // Build the query based on filters
      let query = `
        SELECT
          generateUUIDv4() as id,
          event_time_microseconds as timestamp,
          level as type,
          logger_name as component,
          message,
          concat('Source: ', source_file, ':', toString(source_line), ', Query ID: ', query_id) as details
        FROM system.text_log
        WHERE 1=1
      `;
      
      // Apply tab filter
      if (activeTab === "query") {
        query += `\n AND (logger_name ILIKE '%Query%' OR query_id != '')`;
      } else if (activeTab === "error") {
        query += `\n AND (level = 'Error' OR level = 'Warning' OR level = 'Fatal')`;
      }
      
      // Apply type filter if not "all"
      if (logType !== "all") {
        // Map UI log types to ClickHouse log levels
        const levelMap: Record<string, string> = {
          "ERROR": "Error",
          "WARNING": "Warning",
          "INFO": "Information",
          "DEBUG": "Debug",
          "TRACE": "Trace",
          "FATAL": "Fatal",
        };
        
        const clickhouseLevel = levelMap[logType] || logType;
        query += `\n AND level = '${clickhouseLevel}'`;
      }
      
      if (searchTerm) {
        query += `\n AND (message ILIKE '%${searchTerm}%' OR logger_name ILIKE '%${searchTerm}%' OR source_file ILIKE '%${searchTerm}%')`;
      }
      
      // Apply time range filter
      const { fromDate, toDate } = getTimeRangeDates();
      query += `\n AND event_time_microseconds >= parseDateTimeBestEffort('${fromDate}')`;
      query += `\n AND event_time_microseconds <= parseDateTimeBestEffort('${toDate}')`;
      
      // Order and limit
      query += `
        ORDER BY event_time_microseconds DESC
        LIMIT 1000
      `;
      
      const result = await runQuery(query);
      
      if (result.data && Array.isArray(result.data)) {
        setLogs(result.data as LogEntry[]);
      } else {
        setLogs([]);
      }
    } catch (err: any) {
      setError(err.message || "Failed to fetch logs");
      setLogs([]);
    } finally {
      setIsLoading(false);
    }
  };
  
  // Load logs on initial load and when filters change
  useEffect(() => {
    if (isServerAvailable) {
      fetchLogs();
    }
  }, [isServerAvailable, activeTab, logType, searchTerm, timeRange]);
  
  // Function to download logs
  const downloadLogs = () => {
    if (logs.length === 0) return;
    
    const logData = logs.map((log) => ({
      timestamp: log.timestamp,
      type: log.type,
      component: log.component,
      message: log.message,
      details: log.details,
    }));
    
    const jsonString = JSON.stringify(logData, null, 2);
    const blob = new Blob([jsonString], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    
    const now = new Date().toISOString().split(".")[0].replace(/[:.]/g, "-");
    a.download = `clickhouse-logs-${now}.json`;
    
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };
  
  return (
    <div className="container mx-auto py-6 h-screen flex flex-col">
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Logs</h1>
          <p className="text-muted-foreground">
            View and search ClickHouse server logs
          </p>
        </div>
        <div className="flex gap-2 items-center">
          <Select 
            value={autoRefresh?.toString() || "null"} 
            onValueChange={(value) => setAutoRefresh(value === "null" ? null : Number(value))}
          >
            <SelectTrigger className="w-36">
              <ClockIcon className="mr-2 h-4 w-4" />
              <SelectValue placeholder="Auto-refresh" />
            </SelectTrigger>
            <SelectContent>
              {refreshOptions.map((option) => (
                <SelectItem key={option.value?.toString() || "null"} value={option.value?.toString() || "null"}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          
          <Button
            variant="outline"
            size="sm"
            onClick={fetchLogs}
            disabled={isLoading}
          >
            <RefreshCcw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={downloadLogs}
            disabled={isLoading || logs.length === 0}
          >
            <Download className="h-4 w-4 mr-2" />
            Export
          </Button>
        </div>
      </div>
      
      {error && (
        <Alert variant="destructive" className="mb-4">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      
      <Tabs
        defaultValue="system"
        value={activeTab}
        onValueChange={setActiveTab}
        className="space-y-4 flex-1 flex flex-col w-full"
      >
        <TabsList>
          <TabsTrigger className="w-full" value="system">System Logs</TabsTrigger>
          <TabsTrigger className="w-full" value="query">Query Logs</TabsTrigger>
          <TabsTrigger className="w-full" value="error">Error Logs</TabsTrigger>
        </TabsList>
        
        <Card className="flex-shrink-0">
          <CardHeader className="pb-3">
            <CardTitle>Filters</CardTitle>
            <CardDescription>
              Filter logs by time range, type, and search terms
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div className="space-y-2 col-span-4 md:col-span-2">
                <Label htmlFor="search">Search</Label>
                <div className="relative">
                  <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                  <Input
                    id="search"
                    placeholder="Search message or component..."
                    className="pl-9"
                    value={searchTerm}
                    onChange={(e) => setSearchTerm(e.target.value)}
                  />
                  {searchTerm && (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="absolute right-1 top-1 h-8 w-8 opacity-70 hover:opacity-100"
                      onClick={() => setSearchTerm("")}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  )}
                </div>
              </div>
              
              <div className="space-y-2">
                <Label htmlFor="log-type">Log Type</Label>
                <Select value={logType} onValueChange={setLogType}>
                  <SelectTrigger id="log-type">
                    <SelectValue placeholder="Select log type" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">All Types</SelectItem>
                    <SelectItem value="ERROR">Error</SelectItem>
                    <SelectItem value="FATAL">Fatal</SelectItem>
                    <SelectItem value="WARNING">Warning</SelectItem>
                    <SelectItem value="INFO">Info</SelectItem>
                    <SelectItem value="DEBUG">Debug</SelectItem>
                    <SelectItem value="TRACE">Trace</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              
              <div className="space-y-2">
                <Label htmlFor="time-range">Time Range</Label>
                <Select value={timeRange} onValueChange={setTimeRange}>
                  <SelectTrigger id="time-range">
                    <SelectValue placeholder="Select time range" />
                  </SelectTrigger>
                  <SelectContent>
                    {timeRangeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          </CardContent>
        </Card>
        
        <div className="flex-1 flex flex-col min-h-0" ref={scrollContainerRef}>
          <TabsContent value="system" className="flex-1 flex flex-col mt-0">
            <Card className="flex-1 flex flex-col">
              <CardHeader className="pb-3 flex-shrink-0">
                <CardTitle>System Logs</CardTitle>
                <CardDescription>
                  General logs from the ClickHouse server
                </CardDescription>
              </CardHeader>
              <CardContent className="p-0 flex-1 overflow-hidden">
                <ScrollArea
                  className="h-full"
                  style={{ height: scrollAreaHeight }}
                >
                  <LogTable
                    logs={logs}
                    isLoading={isLoading}
                    onLogClick={handleLogClick}
                  />
                </ScrollArea>
              </CardContent>
            </Card>
          </TabsContent>
          
          <TabsContent value="query" className="flex-1 flex flex-col mt-0">
            <Card className="flex-1 flex flex-col">
              <CardHeader className="pb-3 flex-shrink-0">
                <CardTitle>Query Logs</CardTitle>
                <CardDescription>
                  Logs related to query execution
                </CardDescription>
              </CardHeader>
              <CardContent className="p-0 flex-1 overflow-hidden">
                <ScrollArea
                  className="h-full"
                  style={{ height: scrollAreaHeight }}
                >
                  <LogTable
                    logs={logs}
                    isLoading={isLoading}
                    onLogClick={handleLogClick}
                  />
                </ScrollArea>
              </CardContent>
            </Card>
          </TabsContent>
          
          <TabsContent value="error" className="flex-1 flex flex-col mt-0">
            <Card className="flex-1 flex flex-col">
              <CardHeader className="pb-3 flex-shrink-0">
                <CardTitle>Error Logs</CardTitle>
                <CardDescription>Error and warning messages</CardDescription>
              </CardHeader>
              <CardContent className="p-0 flex-1 overflow-hidden">
                <ScrollArea
                  className="h-full"
                  style={{ height: scrollAreaHeight }}
                >
                  <LogTable
                    logs={logs}
                    isLoading={isLoading}
                    onLogClick={handleLogClick}
                  />
                </ScrollArea>
              </CardContent>
            </Card>
          </TabsContent>
        </div>
      </Tabs>
      
      {/* Log Detail Sheet */}
      <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
        <SheetContent className="w-full sm:max-w-xl overflow-y-auto">
          <SheetHeader>
            <SheetTitle>Log Details</SheetTitle>
            <SheetDescription>
              Detailed information about the selected log
            </SheetDescription>
          </SheetHeader>
          <div className="py-6">
            <LogDetailView log={selectedLog} />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
};

export default LogsPage;