const express = require("express");
const router = express.Router();

const isAuthenticated = require("../middleware/isAuthenticated");

const {
  getDatabasesAndTables,
  getIntellisense,
  getClickHouseFunctions,
  getKeywords,
} = require("../controllers/chUiQueries.controller");

router.get("/databases", isAuthenticated, getDatabasesAndTables);
router.get("/intellisense", isAuthenticated, getIntellisense);
router.get("/functions", isAuthenticated, getClickHouseFunctions);
router.get("/keywords", isAuthenticated, getKeywords);

module.exports = router;
