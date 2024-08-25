const { createClient } = require("@clickhouse/client");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");
const QuerieSave = require("../models/QuerieSave");

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

const getDatabasesTablesAndQueries = async (req, res) => {
  try {
    // Fetch saved queries
    const savedQueries = await QuerieSave.find({
      $or: [{ user: req.user._id }, { public: true }],
    });

    const queryFunc = async (client) => {
      const query = `
        SELECT
          databases.name AS database_name,
          tables.name AS table_name,
          tables.engine AS table_type
        FROM system.databases AS databases
        LEFT JOIN system.tables AS tables
          ON databases.name = tables.database
        ORDER BY database_name, table_name;
      `;

      const result = await client.query({ query, format: "JSON" });
      const resultJSON = await result.json();

      const databases = {};
      resultJSON.data.forEach((row) => {
        const { database_name, table_name, table_type } = row;

        if (!databases[database_name]) {
          databases[database_name] = {
            name: database_name,
            type: "database",
            children: [],
          };
        }

        if (table_name) {
          const table_type_mapped =
            table_type && table_type.toLowerCase() === "view"
              ? "view"
              : "table";

          databases[database_name].children.push({
            name: table_name,
            type: table_type_mapped,
          });
        }
      });

      // Convert databases object to array
      const databasesArray = Object.values(databases).map((database) => ({
        ...database,
        children: database.children.length > 0 ? database.children : [],
      }));

      // Structure saved queries to match databases and tables format
      const savedQueriesStructured = {
        name: "Saved Queries",
        type: "folder",
        children: savedQueries.map((query) => ({
          name: query.name,
          type: "saved_query",
          id: query._id,
          query: query.query,
          public: query.public,
          createdAt: query.createdAt,
          updatedAt: query.updatedAt,
        })),
      };

      // Add saved queries to the response
      return [...databasesArray, savedQueriesStructured];
    };

    const result = await executeClickHouseQuery(req, res, queryFunc);

    // If executeClickHouseQuery doesn't send the response, we can send it here
    if (!res.headersSent) {
      res.json(result);
    }
  } catch (error) {
    console.error("Error in getDatabasesTablesAndQueries:", error);
    if (!res.headersSent) {
      res.status(500).json({
        error:
          "An error occurred while fetching databases, tables, and queries",
      });
    }
  }
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

const getDatabaseTableViewMetrics = async (req, res) => {
  const { database, table, view } = req.query;

  if (!database) {
    return errorResponse(
      res,
      400,
      6001,
      "Database name is required",
      "getDatabaseTableViewMetrics"
    );
  }

  const queryFunc = async (client) => {
    let queries = {};

    if (!table) {
      // Database-level queries
      queries = {
        databaseInfo: `
          SELECT 
            d.name,
            d.engine,
            d.data_path,
            d.metadata_path,
            t.tables,
            t.total_rows,
            t.total_bytes
          FROM system.databases d
          LEFT JOIN (
            SELECT 
              database,
              count() AS tables,
              sum(total_rows) AS total_rows,
              sum(total_bytes) AS total_bytes
            FROM system.tables
            GROUP BY database
          ) AS t ON d.name = t.database
          WHERE d.name = '${database}';
        `,
        tablesInDatabase: `
          SELECT name, engine, total_rows, total_bytes, create_table_query,
                 metadata_modification_time, partition_key, sorting_key, primary_key
          FROM system.tables
          WHERE database = '${database}'
          ORDER BY total_rows DESC;
        `,
        viewsInDatabase: `
          SELECT name, engine, create_table_query
          FROM system.tables
          WHERE database = '${database}' AND engine LIKE '%View'
          ORDER BY name;
        `,
        topColumns: `
          SELECT table, name AS column_name, type AS data_type, 
                 COUNT() OVER (PARTITION BY table) AS columns_in_table
          FROM system.columns
          WHERE database = '${database}'
          ORDER BY table, column_name
          LIMIT 100;
        `,
      };
    } else {
      // Table-level queries
      queries = {
        tableInfo: `
          SELECT *
          FROM system.tables
          WHERE database = '${database}' AND name = '${table}'
        `,
        columns: `
          SELECT name, type, default_kind, default_expression, 
                 comment, compression_codec, is_in_partition_key, 
                 is_in_sorting_key, is_in_primary_key
          FROM system.columns
          WHERE database = '${database}' AND table = '${table}'
        `,
        partitions: `
          SELECT partition, sum(rows) AS rows, sum(bytes) AS bytes,
                 min(min_time) AS min_time, max(max_time) AS max_time
          FROM system.parts
          WHERE database = '${database}' AND table = '${table}'
          GROUP BY partition
          ORDER BY partition
        `,
        dataSample: `
          SELECT *
          FROM ${database}.${table}
          LIMIT 20;
        `,
      };
    }

    const results = await Promise.all(
      Object.entries(queries).map(async ([key, query]) => {
        const result = await client.query({
          query,
          format: "JSONEachRow",
        });
        return { [key]: await result.json() };
      })
    );

    return results.reduce((acc, curr) => ({ ...acc, ...curr }), {});
  };

  return executeClickHouseQuery(req, res, queryFunc);
};

module.exports = {
  getDatabasesTablesAndQueries,
  getIntellisense,
  getClickHouseFunctions,
  getKeywords,
  getDatabaseTableViewMetrics,
};
