import { useEffect } from "react";
import { useClickHouseState } from "@/providers/ClickHouseContext";
//import ReactECharts from "echarts-for-react";

export default function InstanceMetricsPage() {
  const {
    isServerAvailable,
    isLoading,
    version,
    clusterOverviewData,
    clusterOverview,
  } = useClickHouseState();

  useEffect(() => {
    if (isServerAvailable && !clusterOverviewData.length) {
      clusterOverview();
    }
  }, []);

  return (
    <div className="p-4 h-screen flex flex-col">
      <div>
        <h1 className="text-3xl font-bold">Instance Metrics</h1>
        <p className="text-gray-500">
          Check the status of the ClickHouse server and its version.
        </p>
        <div className="mt-4">
          <div className="flex items-center">
            <p className="text-xs">
              Server Status:{" "}
              {isLoading
                ? "Loading..."
                : isServerAvailable
                  ? "Online"
                  : "Offline"}
            </p>
          </div>
          <div className="flex items-center">
            <p className="text-xs">Server Version: {version}</p>
          </div>

          <div className="flex items-center text-xs">
            {clusterOverviewData.map((data, index) => (
              <div key={index}>
                <h2>Cluster: {data.cluster}</h2>
                <ul>
                  <li>Shard Num: {data.shard_num}</li>
                  <li>Shard Weight: {data.shard_weight}</li>
                  <li>Internal Replication: {data.internal_replication}</li>
                  <li>Replica Num: {data.replica_num}</li>
                  <li>Host Name: {data.host_name}</li>
                  <li>Host Address: {data.host_address}</li>
                  <li>Port: {data.port}</li>
                  <li>Is Local: {data.is_local ? "Yes" : "No"}</li>
                  <li>User: {data.user}</li>
                  <li>Default Database: {data.default_database}</li>
                  <li>Errors Count: {data.errors_count}</li>
                  <li>Slowdowns Count: {data.slowdowns_count}</li>
                  <li>
                    Estimated Recovery Time: {data.estimated_recovery_time}
                  </li>
                  <li>Database Shard Name: {data.database_shard_name}</li>
                  <li>Database Replica Name: {data.database_replica_name}</li>
                  <li>Is Active: {data.is_active ? "Yes" : "No"}</li>
                </ul>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
