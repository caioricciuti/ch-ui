const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");

const errorResponse = require("../utils/errorResponse");

exports.executeQuery = async (req, res) => {
  try {
    // Get the user and their active organization
    const user = req.user;
    const activeOrganization = user.activeOrganization;

    if (!activeOrganization) {
      return errorResponse(
        res,
        400,
        6000,
        "No active organization",
        "executeQuery",
        "No active organization"
      );
    }

    // Find the active organization
    const organization = await Organization.findById(activeOrganization);
    if (!organization) {
      return errorResponse(
        res,
        400,
        6000,
        "Active organization not found",
        "executeQuery",
        "No active organization"
      );
    }

    // Find ClickHouse credentials for the user's active organization and user
    const clickHouseCredential = await ClickHouseCredential.findOne({
      allowedOrganizations: organization._id,
      users: user._id,
    });

    if (!clickHouseCredential) {
      return errorResponse(
        res,
        403,
        6002,
        "No ClickHouse credentials found for this organization and user",
        "executeQuery",
        "Unauthorized"
      );
    }

    // Create ClickHouse client
    const clickhouse = createClient({
      url: clickHouseCredential.host,
      username: clickHouseCredential.username,
      password: clickHouseCredential.password,
    });

    // Execute the query
    const { query, queryType } = req.body;
    const result = await clickhouse.query({
      query: query,
      format: "JSON",
    });

    const queryResult = await result.json();

    return res.status(200).json(queryResult);
  } catch (error) {
    return errorResponse(
      res,
      500,
      6001,
      "Failed to execute query",
      "executeQuery",
      error
    );
  }
};
