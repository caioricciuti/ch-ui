// metrics controller for metrics modelconst Metric = require("../models/Metric");
const Organization = require("../models/Organization");
const { body, param, validationResult } = require("express-validator");
const errorResponse = require("../utils/errorResponse");

// Helper function to handle validation errors
const handleValidationErrors = (req, res) => {
  const errors = validationResult(req);
  if (!errors.isEmpty()) {
    return errorResponse(
      res,
      400,
      7001,
      "Validation errors",
      req.route.path,
      errors.array()
    );
  }
  return null;
};

// Get all metrics for the user's active organization
exports.getMetrics = async (req, res) => {
  try {
    const metrics = await Metric.find({
      organization: req.user.activeOrganization,
    })
      .populate("created_by", "name email")
      .sort("-createdAt");
    res.json(metrics);
  } catch (error) {
    console.error("getMetrics error:", error);
    errorResponse(res, 500, 7002, "Failed to fetch metrics", "getMetrics");
  }
};

// Get a single metric by ID
exports.getMetricById = [
  param("id").isMongoId().withMessage("Invalid metric ID"),

  async (req, res) => {
    const validationError = handleValidationErrors(req, res);
    if (validationError) return validationError;

    try {
      const metric = await Metric.findOne({
        _id: req.params.id,
        organization: req.user.activeOrganization,
      }).populate("created_by", "name email");

      if (!metric) {
        return errorResponse(
          res,
          404,
          7003,
          "Metric not found",
          "getMetricById"
        );
      }

      res.json(metric);
    } catch (error) {
      console.error("getMetricById error:", error);
      errorResponse(res, 500, 7004, "Failed to fetch metric", "getMetricById");
    }
  },
];

// Create a new metric
exports.createMetric = [
  body("name").trim().notEmpty().withMessage("Name is required"),
  body("query").notEmpty().withMessage("Query is required"),
  body("description").optional().trim(),
  body("chart_type")
    .isIn(["line", "bar", "pie", "table", "area", "radar", "radial"])
    .withMessage("Invalid chart type"),

  async (req, res) => {
    const validationError = handleValidationErrors(req, res);
    if (validationError) return validationError;

    try {
      const { name, query, description, chart_type } = req.body;

      const metric = new Metric({
        name,
        query,
        description,
        chart_type,
        organization: req.user.activeOrganization,
        created_by: req.user.id,
      });

      await metric.save();
      res.status(201).json(metric);
    } catch (error) {
      console.error("createMetric error:", error);
      errorResponse(res, 500, 7005, "Failed to create metric", "createMetric");
    }
  },
];

// Update a metric
exports.updateMetric = [
  param("id").isMongoId().withMessage("Invalid metric ID"),
  body("name").optional().trim().notEmpty().withMessage("Name cannot be empty"),
  body("query").optional().notEmpty().withMessage("Query cannot be empty"),
  body("description").optional().trim(),
  body("chart_type")
    .optional()
    .isIn(["line", "bar", "pie", "table", "area", "radar", "radial"])
    .withMessage("Invalid chart type"),
  body("active").optional().isBoolean().withMessage("Active must be a boolean"),

  async (req, res) => {
    const validationError = handleValidationErrors(req, res);
    if (validationError) return validationError;

    try {
      const metric = await Metric.findOne({
        _id: req.params.id,
        organization: req.user.activeOrganization,
      });

      if (!metric) {
        return errorResponse(
          res,
          404,
          7003,
          "Metric not found",
          "updateMetric"
        );
      }

      const { name, query, description, chart_type, active } = req.body;

      if (name) metric.name = name;
      if (query) metric.query = query;
      if (description !== undefined) metric.description = description;
      if (chart_type) metric.chart_type = chart_type;
      if (active !== undefined) metric.active = active;

      await metric.save();
      res.json(metric);
    } catch (error) {
      console.error("updateMetric error:", error);
      errorResponse(res, 500, 7006, "Failed to update metric", "updateMetric");
    }
  },
];

// Delete a metric
exports.deleteMetric = [
  param("id").isMongoId().withMessage("Invalid metric ID"),

  async (req, res) => {
    const validationError = handleValidationErrors(req, res);
    if (validationError) return validationError;

    try {
      const metric = await Metric.findOneAndDelete({
        _id: req.params.id,
        organization: req.user.activeOrganization,
      });

      if (!metric) {
        return errorResponse(
          res,
          404,
          7003,
          "Metric not found",
          "deleteMetric"
        );
      }

      res.json({
        message: "Metric deleted successfully",
        deletedMetricId: metric._id,
      });
    } catch (error) {
      console.error("deleteMetric error:", error);
      errorResponse(res, 500, 7007, "Failed to delete metric", "deleteMetric");
    }
  },
];

module.exports = exports;
