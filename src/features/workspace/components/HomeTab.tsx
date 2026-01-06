import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
} from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Github,
  Terminal,
  BookOpen,
  Database,
  ExternalLink,
  UploadCloud,
  Loader2,
} from "lucide-react";
import useAppStore from "@/store";
import { motion } from "framer-motion";
import { Skeleton } from "@/components/ui/skeleton";
import { genTabId } from "@/lib/utils";

const quickStartActions = [
  {
    title: "SQL QUERY",
    icon: <Terminal className="w-5 h-5" />,
    description: "Write and execute SQL queries",
    action: "sql",
  },
];

const resourceCards = [
  {
    title: "SQL Reference",
    description: "Explore ClickHouse SQL documentation.",
    Icon: BookOpen,
    link: "https://clickhouse.com/docs/en/sql-reference",
    action: "Read Docs",
  },
  {
    title: "ClickHouse Docs",
    description: "Learn more about ClickHouse features.",
    Icon: ExternalLink,
    link: "https://clickhouse.com/docs/en/intro",
    action: "Docs",
  },
];

const HomeTab = () => {
  const { addTab, runQuery, credential } = useAppStore();
  const [recentItems, setRecentItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getUsersRecentItems();
  }, [credential?.username]);

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return new Intl.DateTimeFormat("en-US", {
      day: "numeric",
      month: "short",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    }).format(date);
  };

  const handleNewAction = (type: string, query?: string) => {
    addTab({
      id: genTabId(),
      type: "sql",
      title: query
        ? `Recent - ${type}`
        : `New ${type.charAt(0).toUpperCase() + type.slice(1)}`,
      content: query || "",
    });
  };

  const getUsersRecentItems = async () => {
    if (!credential?.username) return;

    setLoading(true);
    setError(null);
    try {
      const recentQueries = await runQuery(`
        SELECT DISTINCT
          replaceAll(query, 'FORMAT JSON', '') AS cleaned_query,
        max(event_time) AS latest_event_time,
        query_kind,
        length(replaceAll(query, 'FORMAT JSON', '')) AS query_length
      FROM
        system.query_log
      WHERE
        user = '${credential.username}'
        AND event_time >= (current_timestamp() - INTERVAL 2 DAY)
        AND arrayExists(db -> db NOT LIKE '%system%', databases)
        AND query NOT LIKE 'SELECT DISTINCT%'
      GROUP BY
        cleaned_query, query_kind
      ORDER BY
        latest_event_time DESC
        LIMIT
          6;
      `);
      setRecentItems(recentQueries.data);
    } catch (err) {
      setError("Failed to load recent queries");
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  const truncateQuery = (query: string, length: number = 50) => {
    return query.length > length ? `${query.slice(0, length)}...` : query;
  };

  return (
    <div className="p-6 space-y-8 max-w-7xl mx-auto">
      <motion.div
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        className="space-y-2"
      >
        <h1 className="text-2xl font-semibold">
          Welcome {credential?.username}
        </h1>
        <p className="text-muted-foreground">Let's get busy...</p>
      </motion.div>

      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 0.1 }}
        className="grid grid-cols-1 md:grid-cols-2 gap-4"
      >
        {quickStartActions.map((action, index) => (
          <Button
            key={index}
            variant="outline"
            className="h-auto p-4 flex flex-col items-start space-y-2 hover:bg-accent hover:text-accent-foreground group"
            onClick={() => handleNewAction(action.action)}
          >
            <div className="flex items-center space-x-2 text-primary">
              <div className="p-2 rounded-full bg-primary/10 group-hover:bg-primary/20">
                {action.icon}
              </div>
              <span className="font-semibold">{action.title}</span>
            </div>
            <span className="text-sm text-muted-foreground text-left">
              {action.description}
            </span>
          </Button>
        ))}
      </motion.div>

      <Tabs defaultValue="recent" className="space-y-4">
        <TabsList className="w-full justify-start">
          <TabsTrigger value="recent" className="flex items-center gap-2">
            Recently opened
            {loading && <Loader2 className="w-3 h-3 animate-spin" />}
          </TabsTrigger>
          <TabsTrigger value="resources">Resources</TabsTrigger>
        </TabsList>

        <TabsContent value="recent" className="space-y-4">
          {loading ? (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {[1, 2, 3].map((i) => (
                <Card key={i} className="space-y-2">
                  <CardHeader>
                    <Skeleton className="h-4 w-[250px]" />
                    <Skeleton className="h-4 w-[200px]" />
                  </CardHeader>
                  <CardFooter>
                    <Skeleton className="h-4 w-[150px]" />
                  </CardFooter>
                </Card>
              ))}
            </div>
          ) : error ? (
            <Card className="p-4 text-center text-muted-foreground">
              {error}
            </Card>
          ) : recentItems.length === 0 ? (
            <Card className="p-4 text-center text-muted-foreground">
              No recent queries found
            </Card>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {recentItems.map((item, index) => (
                <motion.div
                  key={index}
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: index * 0.1 }}
                >
                  <Card
                    className="hover:bg-accent/50 cursor-pointer transition-colors"
                    onClick={() =>
                      handleNewAction(item.query_kind, item.cleaned_query)
                    }
                  >
                    <CardHeader>
                      <CardTitle className="text-sm font-medium flex items-center space-x-2">
                        <Database className="w-4 h-4" />
                        <span>{item.query_kind || "Query"}</span>
                      </CardTitle>
                      <CardDescription className="text-xs font-mono">
                        {truncateQuery(item.cleaned_query)}
                      </CardDescription>
                    </CardHeader>
                    <CardFooter className="text-xs text-muted-foreground">
                      {formatDate(item.latest_event_time)}
                    </CardFooter>
                  </Card>
                </motion.div>
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="resources">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {resourceCards.map((card, index) => (
              <motion.div
                key={index}
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.1 }}
              >
                <Card className="hover:bg-accent/50 transition-colors">
                  <CardHeader>
                    <CardTitle className="text-sm font-medium flex items-center space-x-2">
                      <div className="p-2 rounded-full bg-primary/10">
                        <card.Icon className="w-4 h-4" />
                      </div>
                      <span>{card.title}</span>
                    </CardTitle>
                    <CardDescription className="text-xs">
                      {card.description}
                    </CardDescription>
                  </CardHeader>
                  <CardFooter>
                    <Button
                      variant="ghost"
                      className="w-full"
                      onClick={() => window.open(card.link, "_blank")}
                    >
                      {card.action}
                    </Button>
                  </CardFooter>
                </Card>
              </motion.div>
            ))}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default HomeTab;
