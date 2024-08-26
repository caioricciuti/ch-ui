const express = require("express");
const router = express.Router();

const {
  getMetrics,
  getMetricById,
  createMetric,
  updateMetric,
  deleteMetric,
} = require("../controllers/metrics.controller");

const isAuthenticated = require("../middleware/isAuthenticated");
const isAuthorized = require("../middleware/isAuthorized");

// Apply authentication middleware to all routes
router.use(isAuthenticated);

// Get all metrics for the user's active organization
router.get("/", getMetrics);

// Get a single metric by ID
router.get("/:id", getMetricById);

// Create a new metric
router.post("/", isAuthorized("createMetric"), createMetric);

// Update a metric
router.put("/:id", isAuthorized("updateMetric"), updateMetric);

// Delete a metric
router.delete("/:id", isAuthorized("deleteMetric"), deleteMetric);

module.exports = router;
