const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");

// Helper functions
const checkActiveOrganization = async (user) => {
  if (!user.activeOrganization) {
    throw new Error("No active organization");
  }
  const organization = await Organization.findById(user.activeOrganization);
  if (!organization) {
    throw new Error("Active organization not found");
  }
  if (!organization.members.includes(user._id)) {
    throw new Error("User is not a member of the active organization");
  }
  return organization;
};

const getActiveClickHouseCredential = async (user, organization) => {
  if (!user.activeClickhouseCredential) {
    throw new Error("No active ClickHouse credentials found for this user");
  }
  const credential = await ClickHouseCredential.findById(
    user.activeClickhouseCredential
  ).select("+password");
  if (!credential) {
    throw new Error("ClickHouse credentials not found");
  }
  if (!credential.allowedOrganizations.includes(organization._id)) {
    throw new Error(
      "The active credential is not associated with the active organization"
    );
  }
  if (!credential.users.includes(user._id)) {
    throw new Error("User is not allowed to use this credential");
  }
  return credential;
};

const createClickHouseClient = (credential) => {
  return createClient({
    url: new URL(`${credential.host}:${credential.port}`),
    username: credential.username,
    password: credential.password,
  });
};

// Main controller functions
const executeClickHouseQuery = async (req, res, queryFunc) => {
  try {
    const user = req.user;
    const organization = await checkActiveOrganization(user);
    const credential = await getActiveClickHouseCredential(user, organization);
    const clickhouseClient = createClickHouseClient(credential);
    const result = await queryFunc(clickhouseClient);
    return res.status(200).json(result);
  } catch (error) {
    console.error(`Error in ${queryFunc.name}:`, error);
    return errorResponse(res, 500, 6004, error.message, queryFunc.name);
  }
};

const getDatabasesAndTables = async (req, res) => {
  const queryFunc = async (client) => {
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
    const result = await client.query({ query, format: "JSON" });
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

    return Object.values(databases);
  };

  return executeClickHouseQuery(req, res, queryFunc);
};

const getIntellisense = async (req, res) => {
  const queryFunc = async (client) => {
    const query = `
    SELECT 
    database,
    table,
    name AS column_name,
    type AS column_type
      FROM system.columns
      ORDER BY database, table, column_name;
    `;
    const result = await client.query({ query, format: "JSON" });
    const resultJSON = await result.json();

    const databases = {};
    resultJSON.data.forEach((row) => {
      const { database, table, column_name, column_type } = row;

      if (!databases[database]) {
        databases[database] = {
          name: database,
          type: "database",
          children: [],
        };
      }

      const tableIndex = databases[database].children.findIndex(
        (tableObj) => tableObj.name === table
      );

      if (tableIndex === -1) {
        databases[database].children.push({
          name: table,
          type: "table",
          children: [],
        });
      }

      databases[database].children[
        databases[database].children.length - 1
      ].children.push({
        name: column_name,
        type: column_type,
      });
    });

    return Object.values(databases);
  };

  return executeClickHouseQuery(req, res, queryFunc);
};

const getClickHouseFunctions = async (req, res) => {
  const queryFunc = async (client) => {
    const query = `SELECT name from system.functions`;
    const result = await client.query({ query, format: "JSON" });
    const resultJSON = await result.json();
    const functions = resultJSON.data.map((row) => row.name);
    return functions;
  };

  return executeClickHouseQuery(req, res, queryFunc);
};

const getKeywords = async (req, res) => {
  const queryFunc = async (client) => {
    const query = `SELECT keyword FROM system.keywords`;
    const result = await client.query({ query, format: "JSON" });
    const resultJSON = await result.json();
    const keywords = resultJSON.data.map((row) => row.keyword);
    return keywords;
  };

  return executeClickHouseQuery(req, res, queryFunc);
};

module.exports = {
  getDatabasesAndTables,
  getIntellisense,
  getClickHouseFunctions,
  getKeywords,
};
