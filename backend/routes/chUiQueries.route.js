const express = require("express");
const router = express.Router();

const isAuthenticated = require("../middleware/isAuthenticated");

const {
  getDatabasesTablesAndQueries,
  getIntellisense,
  getClickHouseFunctions,
  getKeywords,
  getDatabaseTableViewMetrics,
} = require("../controllers/chUiQueries.controller");

router.get("/databases", isAuthenticated, getDatabasesTablesAndQueries);
router.get("/intellisense", isAuthenticated, getIntellisense);
router.get("/functions", isAuthenticated, getClickHouseFunctions);
router.get("/keywords", isAuthenticated, getKeywords);
router.get("/metrics", isAuthenticated, getDatabaseTableViewMetrics);

module.exports = router;
