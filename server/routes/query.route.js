const express = require("express");
const router = express.Router();

const isAuthenticated = require("../middleware/isAuthenticated");

const {
  executeQuery,
  saveQuery,
} = require("../controllers/queries.controller");

router.post("/", isAuthenticated, executeQuery);
router.post("/save", isAuthenticated, saveQuery);

module.exports = router;
