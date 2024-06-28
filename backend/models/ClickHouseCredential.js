const mongoose = require("mongoose");

const clickHouseCredentialSchema = new mongoose.Schema(
  {
    users: [
      {
        type: mongoose.Schema.Types.ObjectId,
        ref: "User",
      },
    ],
    name: {
      type: String,
      required: true,
      trim: true,
    },
    slug: {
      type: String,
      required: true,
      unique: true,
      trim: true,
    },
    host: {
      type: String,
      required: true,
    },
    port: {
      type: Number,
      default: 8123,
      required: true,
    },
    username: {
      type: String,
      required: true,
    },
    password: {
      type: String,
      required: true,
    },
    allowedOrganizations: [
      {
        type: mongoose.Schema.Types.ObjectId,
        ref: "Organization",
      },
    ],
    createdAt: {
      type: Date,
      default: Date.now,
    },
    updatedAt: {
      type: Date,
      default: Date.now,
    },
  },
  { timestamps: true }
);

module.exports = mongoose.model(
  "ClickHouseCredential",
  clickHouseCredentialSchema
);
