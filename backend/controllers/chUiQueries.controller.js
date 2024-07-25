const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");

const getDatabasesAndTables = async (req, res) => {
  try {
    const user = req.user;
    const { activeOrganization, activeClickhouseCredential } = user;

    // Check if user has an active organization
    if (!activeOrganization) {
      return errorResponse(
        res,
        400,
        6000,
        "No active organization",
        "getDatabasesAndTables"
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
        "getDatabasesAndTables"
      );
    }

    // Check if user is a member of the active organization
    if (!organization.members.includes(user._id)) {
      return errorResponse(
        res,
        403,
        6006,
        "User is not a member of the active organization",
        "getDatabasesAndTables"
      );
    }

    // Check if user has an active ClickHouse credential
    if (!activeClickhouseCredential) {
      return errorResponse(
        res,
        403,
        6002,
        "No active ClickHouse credentials found for this user",
        "getDatabasesAndTables"
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
        "getDatabasesAndTables"
      );
    }

    // Check if the credential is associated with the active organization
    if (!clickHouseCredential.allowedOrganizations.includes(organization._id)) {
      return errorResponse(
        res,
        403,
        6007,
        "The active credential is not associated with the active organization",
        "getDatabasesAndTables"
      );
    }

    // Check if the user is allowed to use this credential
    if (!clickHouseCredential.users.includes(user._id)) {
      return errorResponse(
        res,
        403,
        6008,
        "User is not allowed to use this credential",
        "getDatabasesAndTables"
      );
    }

    const clickhouseClient = createClient({
      url: new URL(`${clickHouseCredential.host}:${clickHouseCredential.port}`),
      username: clickHouseCredential.username,
      password: clickHouseCredential.password,
    });

    const query = `
      SELECT
        databases.name AS database_name,
        tables.name AS table_name,
        tables.engine AS table_type
      FROM system.databases AS databases
      JOIN system.tables AS tables
        ON databases.name = tables.database
      ORDER BY database_name, table_name;
    `;

    const result = await clickhouseClient.query({ query, format: "JSON" });
    const resultJSON = await result.json();

    const databases = {};
    resultJSON.data.forEach((row) => {
      const { database_name, table_name, table_type } = row;
      const table_type_mapped =
        table_type.toLowerCase() === "view" ? "view" : "table";

      if (!databases[database_name]) {
        databases[database_name] = {
          name: database_name,
          type: "database",
          children: [],
        };
      }

      databases[database_name].children.push({
        name: table_name,
        type: table_type_mapped,
      });
    });

    const mockDatabaseData = Object.values(databases);

    return res.status(200).json(mockDatabaseData);
  } catch (error) {
    console.error("Error in getDatabasesAndTables:", error);
    return errorResponse(
      res,
      500,
      6004,
      error.message,
      "getDatabasesAndTables"
    );
  }
};

module.exports = {
  getDatabasesAndTables,
};
