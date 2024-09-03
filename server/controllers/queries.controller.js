const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");
const QuerieSave = require("../models/QuerieSave");

// Keywords categorized for different query types
const DDL_KEYWORDS = [
  "CREATE",
  "ALTER",
  "DROP",
  "TRUNCATE",
  "RENAME",
  "ATTACH",
  "DETACH",
  "OPTIMIZE",
  "GRANT",
  "REVOKE",
  "SET",
  "SHOW",
];

const INSERT_KEYWORDS = ["INSERT", "REPLACE"];

const SELECT_KEYWORDS = ["SELECT", "EXPLAIN", "WITH", "DESCRIBE"];

/**
 * Determine the type of the query based on its first keyword
 * @param {string} query - The SQL query string
 * @returns {string} - Type of the query ('select', 'insert', 'ddl', 'other')
 */
const determineQueryType = (query) => {
  const upperQuery = query.trim().toUpperCase();

  // Regex to capture the first keyword of the query
  const match = upperQuery.match(/^\s*(\w+)/);
  if (!match || !match[1]) return "other";

  const firstKeyword = match[1];

  // Check if the first keyword matches known patterns
  if (SELECT_KEYWORDS.includes(firstKeyword)) return "select";
  if (INSERT_KEYWORDS.includes(firstKeyword)) return "insert";
  if (DDL_KEYWORDS.includes(firstKeyword)) return "ddl";

  // Default to 'other' if no match
  return "other";
};

const executeClickHouseQuery = async (client, query, queryType) => {
  try {
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
  } catch (error) {
    console.error("Error executing ClickHouse query:", error);
    throw new Error("Failed to execute ClickHouse query.");
  }
};

exports.executeQuery = async (req, res) => {
  let clickhouseClient;

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

    if (!organization.members.includes(user._id)) {
      return errorResponse(
        res,
        403,
        6006,
        "User is not a member of the active organization",
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
    ).select("+password");

    if (!clickHouseCredential) {
      return errorResponse(
        res,
        403,
        6003,
        "ClickHouse credentials not found",
        "executeQuery"
      );
    }

    if (!clickHouseCredential.allowedOrganizations.includes(organization._id)) {
      return errorResponse(
        res,
        403,
        6007,
        "The active credential is not associated with the active organization",
        "executeQuery"
      );
    }

    if (!clickHouseCredential.users.includes(user._id)) {
      return errorResponse(
        res,
        403,
        6008,
        "User is not allowed to use this credential",
        "executeQuery"
      );
    }

    const { query } = req.body;
    if (!query || typeof query !== "string") {
      return errorResponse(res, 400, 6005, "Invalid query", "executeQuery");
    }

    const queryType = determineQueryType(query);
    const userPermissions = user.role;

    if (
      (queryType === "insert" || queryType === "ddl") &&
      userPermissions === "viewer"
    ) {
      return errorResponse(
        res,
        403,
        6009,
        "User does not have permission to execute this type of query",
        "executeQuery"
      );
    }

    clickhouseClient = createClient({
      url: `${clickHouseCredential.host}:${clickHouseCredential.port}`,
      username: clickHouseCredential.username,
      password: clickHouseCredential.password,
    });

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
      try {
        queryResult = await result.json();
      } catch (error) {
        console.error("Error parsing JSON response:", error);
        throw new Error("Failed to parse query result.");
      }
    }

    return res.status(200).json(queryResult);
  } catch (error) {
    console.error("Error executing query:", error);
    return errorResponse(
      res,
      500,
      6004,
      error.message || "Failed to execute query",
      "executeQuery"
    );
  } finally {
    // Close ClickHouse client connection gracefully
    if (clickhouseClient) {
      try {
        await clickhouseClient.close();
      } catch (closeError) {
        console.error("Error closing ClickHouse client:", closeError);
      }
    }
  }
};

exports.saveQuery = async (req, res) => {
  try {
    const { user } = req;
    const { activeOrganization } = user;

    if (!activeOrganization) {
      return errorResponse(
        res,
        400,
        6010,
        "No active organization",
        "saveQuery"
      );
    }

    const organization = await Organization.findById(activeOrganization);
    if (!organization) {
      return errorResponse(
        res,
        404,
        6011,
        "Active organization not found",
        "saveQuery"
      );
    }

    if (!organization.members.includes(user._id)) {
      return errorResponse(
        res,
        403,
        6016,
        "User is not a member of the active organization",
        "saveQuery"
      );
    }

    const { name, query, public } = req.body;
    if (
      !name ||
      !query ||
      typeof name !== "string" ||
      typeof query !== "string"
    ) {
      return errorResponse(res, 400, 6015, "Invalid request body", "saveQuery");
    }

    const savedQuery = new QuerieSave({
      name,
      query,
      user: user._id,
      public: !!public,
      organization: organization._id,
    });

    await savedQuery.save();

    return res.status(201).json(savedQuery);
  } catch (error) {
    console.error("Error saving query:", error);
    return errorResponse(res, 500, 6014, "Failed to save query.", "saveQuery");
  }
};

module.exports = exports;
