import React, { useEffect, useState } from "react";
import { format } from "date-fns";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../ui/card";
import { Badge } from "../ui/badge";
import { ScrollArea } from "../ui/scroll-area";
import { Skeleton } from "../ui/skeleton";
import {
  FileCode2,
  Bug,
  Zap,
  Star,
  GitBranch,
  Calendar,
} from "lucide-react";

type ReleaseType = "feature" | "bugfix" | "improvement" | "breaking";

interface GitHubRelease {
  tag_name: string;
  name: string;
  body: string;
  published_at: string;
  html_url: string;
  assets: Array<{
    download_count: number;
    browser_download_url: string;
  }>;
}

interface ParsedRelease {
  version: string;
  title: string;
  type: ReleaseType;
  date: string;
  url: string;
}

const useGitHubReleases = (owner: string, repo: string) => {
  const [releases, setReleases] = useState<ParsedRelease[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchReleases = async () => {
      try {
        const response = await fetch(
          `https://api.github.com/repos/${owner}/${repo}/releases`
        );

        if (!response.ok) {
          throw new Error("Failed to fetch releases");
        }

        const data: GitHubRelease[] = await response.json();

        const parsedReleases = data.map((release): ParsedRelease => {
          // Parse release type from body using conventional commits
          const type = release.body.toLowerCase().includes("feat")
            ? "feature"
            : release.body.toLowerCase().includes("fix")
            ? "bugfix"
            : release.body.toLowerCase().includes("break")
            ? "breaking"
            : "improvement";

          return {
            version: release.tag_name,
            title: release.name || release.tag_name,
            type,
            date: release.published_at,
            url: release.html_url,
          };
        });

        setReleases(parsedReleases);
        setLoading(false);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to fetch releases"
        );
        setLoading(false);
      }
    };

    fetchReleases();
  }, [owner, repo]);

  return { releases, loading, error };
};

const getUpdateIcon = (type: ReleaseType) => {
  switch (type) {
    case "feature":
      return <Star className="h-4 w-4 text-green-500" />;
    case "bugfix":
      return <Bug className="h-4 w-4 text-red-500" />;
    case "improvement":
      return <Zap className="h-4 w-4 text-blue-500" />;
    case "breaking":
      return <FileCode2 className="h-4 w-4 text-yellow-500" />;
    default:
      return <FileCode2 className="h-4 w-4" />;
  }
};

const ReleaseCard = ({ release }: { release: ParsedRelease }) => (
  <div className="flex gap-4 p-4 hover:bg-muted/50 rounded-lg transition-colors">
    <div className="mt-1">{getUpdateIcon(release.type)}</div>
    <div className="space-y-2 flex-1">
      <div className="flex items-center gap-2 flex-wrap">
        <Badge variant="outline" className="font-mono text-xs">
          {release.version}
        </Badge>
        <span className="font-semibold">{release.title}</span>
      </div>

      <div className="flex items-center gap-4 text-xs text-muted-foreground">
        <div className="flex items-center gap-1">
          <Calendar className="h-3 w-3" />
          <span>{format(new Date(release.date), "MMM d, yyyy")}</span>
        </div>
        <a
          href={release.url}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-1 hover:text-primary"
        >
          <GitBranch className="h-3 w-3" />
          <span>View on GitHub</span>
        </a>
      </div>
    </div>
  </div>
);

const LoadingSkeleton = () => (
  <div className="space-y-8">
    {[1, 2, 3].map((i) => (
      <div key={i} className="flex gap-4 p-4">
        <Skeleton className="h-4 w-4 rounded-full" />
        <div className="space-y-2 flex-1">
          <div className="flex items-center gap-2">
            <Skeleton className="h-5 w-20" />
            <Skeleton className="h-5 w-40" />
          </div>
          <Skeleton className="h-4 w-full" />
          <div className="flex gap-4">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-4 w-24" />
          </div>
        </div>
      </div>
    ))}
  </div>
);

export default function LatestReleases({
  owner = "caioricciuti",
  repo = "ch-ui",
}: {
  owner?: string;
  repo?: string;
}) {
  const { releases, loading, error } = useGitHubReleases(owner, repo);

  return (
    <Card className="w-full mb-12 md:mb-24">
      <CardHeader>
        <CardTitle className="text-2xl font-bold">Latest Releases</CardTitle>
        <CardDescription>
          Stay up to date with our latest changes and improvements
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ScrollArea className="h-[600px] pr-4">
          {error ? (
            <div className="text-center text-red-500 p-4">{error}</div>
          ) : loading ? (
            <LoadingSkeleton />
          ) : (
            <div className="space-y-2">
              {releases.map((release, index) => (
                <ReleaseCard key={index} release={release} />
              ))}
            </div>
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
