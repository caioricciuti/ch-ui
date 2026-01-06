import { AlertTriangleIcon } from "lucide-react";
import { Metrics } from "./metricsConfig";

// Helper function to create a consistent chart config object
const createChartConfig = (
    indexBy: string,
    valueKey: string,
    label: string,
    color: string = "#447EBC",
) => {
    return {
        indexBy,
        [valueKey]: {
            label,
            color,
        },
    };
};

export const exceptionsConfig: Metrics = {
    title: "Query Exceptions",
    scope: "exceptions",
    description: "Overview of query exceptions in the system.",
    icon: AlertTriangleIcon,
    items: [
        {
            title: "Recent Exceptions",
            query: `
          SELECT 
            event_time, 
            query, 
            exception 
          FROM 
            system.query_log 
          WHERE 
            type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time BETWEEN $__timeFromTo 
            AND user = currentUser()
          ORDER BY 
            event_time DESC 
          LIMIT 10
        `,
            type: "table",
            description: "Last 10 exceptions for the current user in the selected time range.",
            tiles: 4,
            showTableStatistics: false,
            hideTableHeader: true,
        },
    ],
};
