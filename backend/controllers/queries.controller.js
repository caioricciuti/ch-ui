const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");

exports.executeQuery = async (req, res) => {
  try {
    const user = req.user;
    const activeOrganization = user.activeOrganization;

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

    if (!user.activeClickhouseCredential) {
      return errorResponse(
        res,
        403,
        6002,
        "No active ClickHouse credentials found for this user",
        "executeQuery"
      );
    }

    const clickHouseCredential = await ClickHouseCredential.findById(
      user.activeClickhouseCredential
    );

    if (!clickHouseCredential) {
      return errorResponse(
        res,
        403,
        6003,
        "No ClickHouse credentials found for this organization and user",
        "executeQuery"
      );
    }

    const clickhouseClient = createClient({
      url: clickHouseCredential.host,
      port: clickHouseCredential.port,
      username: clickHouseCredential.username,
      password: clickHouseCredential.password,
    });

    const { query } = req.body;
    const queryType = determineQueryType(query);

    let result;
    switch (queryType) {
      case "select":
        result = await clickhouseClient.query({
          query,
          format: "JSON",
        });
        break;
      case "insert":
        result = await clickhouseClient.command({
          query,
          clickhouse_settings: {
            wait_end_of_query: 1,
          },
        });
        break;
      case "ddl":
        result = await clickhouseClient.command({
          query,
          clickhouse_settings: {
            wait_end_of_query: 1,
          },
        });
        break;
      default:
        result = await clickhouseClient.query({
          query,
          format: "JSON",
        });
    }

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
      "executeQuery",
      error.message
    );
  }
};

function determineQueryType(query) {
  const upperQuery = query.trim().toUpperCase();
  if (upperQuery.startsWith("SELECT")) {
    return "select";
  } else if (upperQuery.startsWith("INSERT")) {
    return "insert";
  } else if (
    upperQuery.startsWith("CREATE") ||
    upperQuery.startsWith("ALTER") ||
    upperQuery.startsWith("DROP")
  ) {
    return "ddl";
  } else {
    return "other";
  }
}
