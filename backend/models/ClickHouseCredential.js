// ClickHouseCredential Model
const mongoose = require("mongoose");

const clickHouseCredentialSchema = new mongoose.Schema(
  {
    name: {
      type: String,
      required: [true, "Name is required"],
      trim: true,
    },
    slug: {
      type: String,
      required: [true, "Slug is required"],
      unique: true,
      trim: true,
      lowercase: true,
    },
    host: {
      type: String,
      required: [true, "Host is required"],
    },
    port: {
      type: Number,
      default: 8123,
      required: [true, "Port is required"],
    },
    username: {
      type: String,
      required: [true, "Username is required"],
    },
    password: {
      type: String,
      required: [true, "Password is required"],
      select: false, // Don't return password by default in queries
    },
    users: [
      {
        type: mongoose.Schema.Types.ObjectId,
        ref: "User",
      },
    ],
    allowedOrganizations: [
      {
        type: mongoose.Schema.Types.ObjectId,
        ref: "Organization",
      },
    ],
  },
  { timestamps: true }
);

const ClickHouseCredential = mongoose.model(
  "ClickHouseCredential",
  clickHouseCredentialSchema
);

module.exports = ClickHouseCredential;
