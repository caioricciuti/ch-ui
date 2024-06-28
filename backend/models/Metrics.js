// metrics mongoose schema

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
      required: true,
    },
    name: {
      type: String,
      required: true,
    },
    description: {
      type: String,
    },
    updatedAt: {
      type: Date,
      default: Date.now,
    },
    createdAt: {
      type: Date,
      default: Date.now,
    },
    active: {
      type: Boolean,
      default: true,
    },
    chart_type: {
      type: String,
      enum: ["line", "bar", "pie", "table"],
      default: "line",
    },
  },
  { timestamps: true }
);

module.exports = mongoose.model("Metrics", metricsSchema);
