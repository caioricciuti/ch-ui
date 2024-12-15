import { useState, useEffect } from "react";
import { format } from "date-fns";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Bug,
  Zap,
  Star,
  GitBranch,
  Calendar,
  ArrowUpRight,
  AlertTriangle,
} from "lucide-react";

// Types
type ReleaseType = "feature" | "bugfix" | "improvement" | "breaking";

interface Release {
  version: string;
  title: string;
  type: ReleaseType;
  date: string;
  url: string;
}

interface GitHubRelease {
  tag_name: string;
  name: string;
  body: string;
  published_at: string;
  html_url: string;
}

// Constants
const RELEASE_TYPE_CONFIG = {
  feature: {
    icon: Star,
    label: "New Feature",
    color: "text-green-500/90",
  },
  bugfix: {
    icon: Bug,
    label: "Bug Fix",
    color: "text-red-500/90",
  },
  improvement: {
    icon: Zap,
    label: "Improvement",
    color: "text-blue-500/90",
  },
  breaking: {
    icon: AlertTriangle,
    label: "Breaking Change",
    color: "text-yellow-500/90",
  },
} as const;

// Hooks
const useGitHubReleases = (owner: string, repo: string) => {
  const [releases, setReleases] = useState<Release[]>([]);
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
        const parsedReleases = data.map((release): Release => {
          const typeMatch = release.body?.match(
            /Type:\s*(feature|bugfix|improvement|breaking)/i
          );
          
          return {
            version: release.tag_name,
            title: release.name || release.tag_name,
            type: (typeMatch?.[1].toLowerCase() as ReleaseType) || "improvement",
            date: release.published_at,
            url: release.html_url,
          };
        });

        setReleases(parsedReleases);
        setError(null);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "An error occurred while fetching releases"
        );
      } finally {
        setLoading(false);
      }
    };

    void fetchReleases();
  }, [owner, repo]);

  return { releases, loading, error };
};

// Components
const ReleaseIcon = ({ type }: { type: ReleaseType }) => {
  const config = RELEASE_TYPE_CONFIG[type];
  const Icon = config.icon;
  return <Icon className={`h-5 w-5 ${config.color}`} />;
};

interface ReleaseCardProps {
  release: Release;
  isLatest: boolean;
}

const ReleaseCard = ({ release, isLatest }: ReleaseCardProps) => {
  const typeConfig = RELEASE_TYPE_CONFIG[release.type];
  const Icon = typeConfig.icon;

  return (
    <div className="group relative flex gap-3 sm:gap-6 p-3 sm:p-4 hover:bg-muted/50 rounded-xl transition-all duration-200">
      {/* Timeline connector */}
      <div className="absolute -left-px top-6 bottom-0 w-px bg-gradient-to-b from-muted-foreground/20 to-transparent" />
      
      {/* Icon and version badge section */}
      <div className="flex flex-col items-center gap-2">
        <div className="relative flex items-center justify-center">
          <div className="absolute -left-[23px] h-2 w-2 rounded-full bg-muted-foreground/20 ring-4 ring-background" />
          <div className={`p-1.5 sm:p-2 rounded-lg bg-muted/50 ${typeConfig.color.replace('text-', 'text-opacity-90')}`}>
            <Icon className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
          </div>
        </div>
        <div className="flex flex-col items-center gap-1">
          <Badge 
            variant={isLatest ? "default" : "outline"} 
            className="font-mono text-[10px] sm:text-xs whitespace-nowrap"
          >
            {release.version}
          </Badge>
          {isLatest && (
            <Badge variant="secondary" className="text-[10px] sm:text-xs whitespace-nowrap">
              Latest
            </Badge>
          )}
        </div>
      </div>

      {/* Content section */}
      <div className="space-y-1.5 sm:space-y-2 flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <Badge variant="secondary" className="text-[10px] sm:text-xs">
            {typeConfig.label}
          </Badge>
        </div>

        <h3 className="font-semibold text-sm sm:text-base leading-snug truncate">
          {release.title}
        </h3>

        <div className="flex flex-col sm:flex-row items-start sm:items-center gap-2 sm:gap-4 text-xs sm:text-sm text-muted-foreground">
          <div className="flex items-center gap-1.5">
            <Calendar className="h-3 w-3 sm:h-3.5 sm:w-3.5" />
            <span className="whitespace-nowrap">{format(new Date(release.date), "MMM d, yyyy")}</span>
          </div>
          <a
            href={release.url}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 hover:text-primary transition-colors group-hover:text-primary"
          >
            <GitBranch className="h-3 w-3 sm:h-3.5 sm:w-3.5" />
            <span className="underline-offset-4 group-hover:underline whitespace-nowrap">
              View on GitHub
            </span>
            <ArrowUpRight className="h-3 w-3 sm:h-3.5 sm:w-3.5 opacity-0 -translate-y-1 transition-all group-hover:opacity-100 group-hover:translate-y-0" />
          </a>
        </div>
      </div>
    </div>
  );
};

const LoadingSkeleton = () => (
  <div className="space-y-6 sm:space-y-8 px-3 sm:px-6">
    {Array.from({ length: 3 }).map((_, i) => (
      <div key={i} className="flex gap-3 sm:gap-6 py-3 sm:py-4">
        <Skeleton className="h-4 w-4 sm:h-5 sm:w-5 rounded-full" />
        <div className="space-y-2 sm:space-y-3 flex-1">
          <div className="flex items-center gap-2 sm:gap-3">
            <Skeleton className="h-4 w-16 sm:h-5 sm:w-24" />
            <Skeleton className="h-4 w-24 sm:h-5 sm:w-32" />
          </div>
          <Skeleton className="h-5 w-2/3 sm:h-6 sm:w-3/4" />
          <div className="flex flex-col sm:flex-row gap-2 sm:gap-6">
            <Skeleton className="h-4 w-24 sm:h-5 sm:w-36" />
            <Skeleton className="h-4 w-24 sm:h-5 sm:w-36" />
          </div>
        </div>
      </div>
    ))}
  </div>
);

// Main component
export default function Updates() {
  const { releases, loading, error } = useGitHubReleases("caioricciuti", "ch-ui");

  return (
    <Card className="w-full max-w-5xl mx-auto mb-12 h-[250px] overflow-hidden">
      <CardHeader className="space-y-2 sm:space-y-4 py-3 sm:py-4">
        <div className="space-y-1 sm:space-y-2">
          <CardTitle className="text-2xl sm:text-3xl font-bold tracking-tight">
            Latest Updates
          </CardTitle>
          <CardDescription className="text-sm sm:text-base">
            Stay up to date with our latest changes and improvements
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <ScrollArea className="h-[140px] sm:h-[160px] pr-3 sm:pr-6">
          {error ? (
            <div className="flex items-center justify-center h-full text-center rounded-lg border border-destructive/50 bg-destructive/10 p-4 sm:p-8">
              <p className="text-destructive text-xs sm:text-sm">{error}</p>
            </div>
          ) : loading ? (
            <LoadingSkeleton />
          ) : (
            <div className="relative space-y-1 sm:space-y-2 pl-6">
              {releases.map((release, index) => (
                <ReleaseCard
                  key={release.version}
                  release={release}
                  isLatest={index === 0}
                />
              ))}
            </div>
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
