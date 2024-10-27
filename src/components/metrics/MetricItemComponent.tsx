import { useState, useEffect } from "react";
import { MetricItem } from "@/helpers/metricsConfig";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import CHUITable from "@/components/table/CHUItable";
import { Skeleton } from "../ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  CopyIcon,
  InfoIcon,
  EllipsisVertical,
  DownloadCloud,
  Braces,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import ErrorCard from "@/components/metrics/ErrorCard";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import useAppStore from "@/store/appStore";
import ChartComponent from "./ChartComponent";

interface Props {
  item: MetricItem;
}

function MetricItemComponent({ item }: Props) {
  const { runQuery } = useAppStore();
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [errorMessage, setErrorMessage] = useState<string>("");

  const fetchData = async () => {
    try {
      setLoading(true);
      const result = await runQuery(item.query);

      if (result.error) {
        setData([]);
        setErrorMessage(result.error);
      } else if (result.data && result.data.length > 0) {
        setData(result.data);
        setErrorMessage("");
      } else {
        setData([]);
        setErrorMessage("No data returned from the query.");
      }
    } catch (err: any) {
      console.error(err);
      setData([]);
      setErrorMessage(err.message || "An unexpected error occurred.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, [item.query, runQuery]);

  if (loading) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className="h-6 w-3/4" />
        </CardHeader>
        <CardContent>
          <Skeleton className="h-[250px] w-full" />
        </CardContent>
      </Card>
    );
  }

  if (errorMessage) {
    return (
      <ErrorCard
        item={item}
        errorMessage={errorMessage}
        fetchData={fetchData}
      />
    );
  }

  const renderCardContent = () => {
    if (!data || !data.data || data.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }
    return data.data.map((item: any, index: number) => (
      <div key={index} className="mb-2">
        {Object.entries(item).map(([key, value]) => (
          <div key={key}>
            <span className="font-extrabold text-3xl">{value?.toString()}</span>
          </div>
        ))}
      </div>
    ));
  };

  const handleDownloadData = (data: any) => {
    const blob = new Blob([JSON.stringify(data, null, 2)], {
      type: "application/json",
    });
    const fileName = `${item.title.replace(" ", "_").toLowerCase()}.json`;
    const link = document.createElement("a");
    if (link.download !== undefined) {
      const url = URL.createObjectURL(blob);
      link.setAttribute("href", url);
      link.setAttribute("download", fileName);
      link.style.visibility = "hidden";
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
    }
  };

  const renderChart = () => {
    if (!data || !data.data || data.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }

    const chartConfig = item.chartConfig || { indexBy: "name", data: {} };

    return <ChartComponent chartType={item.chartType} data={data.data} config={chartConfig} />;
  };

  const renderTable = () => {
    if (!data || !data.data || data.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }
    return (
      <CHUITable
        result={{
          meta: data.meta,
          data: data.data,
          statistics: {
            elapsed: data.statistics.time_elapsed,
            rows_read: data.statistics.rows_read,
            bytes_read: JSON.stringify(data).length,
          },
        }}
      />
    );
  };

  const renderContent = () => {
    switch (item.type) {
      case "card":
        return renderCardContent();
      case "chart":
        return renderChart();
      case "table":
        return renderTable();
      default:
        return null;
    }
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg truncate">{item.title}</CardTitle>
          <div className="flex items-center space-x-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <InfoIcon className="w-5 h-5 text-muted-foreground cursor-pointer" />
                </TooltipTrigger>
                <TooltipContent>
                  <div className="p-2 max-w-[350px]">
                    <p className="text-sm text-muted-foreground">
                      {item.description}
                    </p>
                  </div>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon">
                  <EllipsisVertical className="w-4 h-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onClick={() => {
                    navigator.clipboard.writeText(item.query);
                    toast.success("Query copied to clipboard");
                  }}
                >
                  <CopyIcon className="w-4 h-4 mr-2" />
                  Copy Query
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleDownloadData(data)}>
                  <DownloadCloud className="w-4 h-4 mr-2" />
                  Download Data
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    const jsonString = JSON.stringify(data.data, null, 2);
                    navigator.clipboard
                      .writeText(jsonString)
                      .then(() => toast.success("Data copied to clipboard"))
                      .catch(() =>
                        toast.error("Failed to copy data to clipboard")
                      );
                  }}
                >
                  <Braces className="w-4 h-4 mr-2" />
                  Copy Data
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </CardHeader>
      <CardContent className="truncate">{renderContent()}</CardContent>
    </Card>
  );
}

export default MetricItemComponent;
