const mongoose = require("mongoose");

const metricsSchema = new mongoose.Schema(
  {
    organization: {
      type: mongoose.Schema.Types.ObjectId,
      ref: "Organization",
      required: true,
    },
    created_by: {
      type: mongoose.Schema.Types.ObjectId,
      ref: "User",
      required: true,
    },
    query: {
      type: String,
      required: [true, "Query is required"],
    },
    name: {
      type: String,
      required: [true, "Name is required"],
      trim: true,
    },
    description: {
      type: String,
      trim: true,
    },
    active: {
      type: Boolean,
      default: true,
    },
    chart_type: {
      type: String,
      enum: ["line", "bar", "pie", "table", "area", "radar", "radial"],
      default: "line",
    },
  },
  { timestamps: true }
);

const Metric = mongoose.model("Metric", metricsSchema);

module.exports = Metric;
