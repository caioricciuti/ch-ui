const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");
const QuerieSave = require("../models/QuerieSave");

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

    // Check if user has an active organization
    if (!activeOrganization) {
      return errorResponse(
        res,
        400,
        6000,
        "No active organization",
        "executeQuery"
      );
    }

    // Fetch the active organization
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

    // Check if user is a member of the active organization
    if (!organization.members.includes(user._id)) {
      return errorResponse(
        res,
        403,
        6006,
        "User is not a member of the active organization",
        "executeQuery"
      );
    }

    // Check if user has an active ClickHouse credential
    if (!activeClickhouseCredential) {
      return errorResponse(
        res,
        403,
        6002,
        "No active ClickHouse credentials found for this user",
        "executeQuery"
      );
    }

    // Fetch the active ClickHouse credential
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

    // Check if the credential is associated with the active organization
    if (!clickHouseCredential.allowedOrganizations.includes(organization._id)) {
      return errorResponse(
        res,
        403,
        6007,
        "The active credential is not associated with the active organization",
        "executeQuery"
      );
    }

    // Check if the user is allowed to use this credential
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

    // Check user's permission to execute this type of query
    const userPermissions = user.role; // Assuming user roles are 'admin', 'user', or 'viewer'
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

    const clickhouseClient = createClient({
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
      queryResult = await result.json();
    }

    return res.status(200).json(queryResult);
  } catch (error) {
    return errorResponse(
      res,
      400,
      6004,
      error.message.toString(),
      "executeQuery"
    );
  }
};

exports.saveQuery = async (req, res) => {
  try {
    const { user } = req;
    const { activeOrganization } = user;

    // Check if user has an active organization
    if (!activeOrganization) {
      return errorResponse(
        res,
        400,
        6010,
        "No active organization",
        "saveQuery"
      );
    }

    // Fetch the active organization
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

    // Check if user is a member of the active organization
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
    if (!name || !query || typeof name !== "string" || typeof query !== "string") {
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
    return errorResponse(
      res,
      400,
      6014,
      error.message.toString(),
      "saveQuery"
    );
  }
}


module.exports = exports;
