const express = require("express");
const router = express.Router();

const isAuthenticated = require("../middleware/isAuthenticated");

const { executeQuery } = require("../controllers/queries.controller");

router.post("/query", isAuthenticated, executeQuery);

module.exports = router;
