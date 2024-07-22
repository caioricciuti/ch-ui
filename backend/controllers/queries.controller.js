const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");

const determineQueryType = (query) => {
  const upperQuery = query.trim().toUpperCase();
  if (upperQuery.startsWith("SELECT")) return "select";
  if (upperQuery.startsWith("INSERT")) return "insert";
  if (
    upperQuery.startsWith("CREATE") ||
    upperQuery.startsWith("ALTER") ||
    upperQuery.startsWith("DROP")
  )
    return "ddl";
  return "other";
};

const executeClickHouseQuery = async (client, query, queryType) => {
  switch (queryType) {
    case "select":
      return client.query({ query, format: "JSON" });
    case "insert":
    case "ddl":
      return client.command({
        query,
        clickhouse_settings: { wait_end_of_query: 1 },
      });
    default:
      return client.query({ query, format: "JSON" });
  }
};

exports.executeQuery = async (req, res) => {
  try {
    const { user } = req;
    const { activeOrganization, activeClickhouseCredential } = user;

    if (!activeOrganization) {
      return errorResponse(
        res,
        400,
        6000,
        "No active organization",
        "executeQuery"
      );
    }

    const organization = await Organization.findById(activeOrganization);
    if (!organization) {
      return errorResponse(
        res,
        404,
        6001,
        "Active organization not found",
        "executeQuery"
      );
    }

    if (!activeClickhouseCredential) {
      return errorResponse(
        res,
        403,
        6002,
        "No active ClickHouse credentials found for this user",
        "executeQuery"
      );
    }

    const clickHouseCredential = await ClickHouseCredential.findById(
      activeClickhouseCredential
    );
    if (!clickHouseCredential) {
      return errorResponse(
        res,
        403,
        6003,
        "ClickHouse credentials not found",
        "executeQuery"
      );
    }

    const { query } = req.body;
    if (!query || typeof query !== "string") {
      return errorResponse(res, 400, 6005, "Invalid query", "executeQuery");
    }

    const clickhouseClient = createClient({
      url: clickHouseCredential.host,
      port: clickHouseCredential.port,
      username: clickHouseCredential.username,
      password: clickHouseCredential.password,
    });

    const queryType = determineQueryType(query);
    const result = await executeClickHouseQuery(
      clickhouseClient,
      query,
      queryType
    );

    let queryResult;
    if (queryType === "insert" || queryType === "ddl") {
      queryResult = {
        message: "Query executed successfully",
        query_id: result.query_id,
      };
    } else {
      queryResult = await result.json();
    }

    return res.status(200).json(queryResult);
  } catch (error) {
    console.error("Error in executeQuery:", error);
    return errorResponse(
      res,
      500,
      6004,
      "Failed to execute query",
      "executeQuery"
    );
  }
};

module.exports = exports;
