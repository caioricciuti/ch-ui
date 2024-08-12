const express = require("express");
const router = express.Router();

const isAuthenticated = require("../middleware/isAuthenticated");
const isAdmin = require("../middleware/isAdmin");
const isAuthorized = require("../middleware/isAuthorized");

const {
  getDatabasesAndTables,
  getIntellisense
} = require("../controllers/chUiQueries.controller");


router.get("/databases", isAuthenticated, getDatabasesAndTables);
router.get("/intellisense", isAuthenticated, getIntellisense);

module.exports = router;
