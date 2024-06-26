const mongoose = require("mongoose");

const userSchema = new mongoose.Schema(
  {
    name: {
      type: String,
      trim: true,
      required: true,
      maxlength: 32,
    },
    email: {
      type: String,
      trim: true,
      required: true,
      unique: true,
    },
    password: {
      type: String,
      required: true,
    },
    role: {
      type: String,
      enum: ["admin", "user"],
      default: "user",
    },
    createdAt: {
      type: Date,
      default: Date.now,
    },
    updatedAt: {
      type: Date,
      default: Date.now,
    },
    active: {
      type: Boolean,
      default: false,
    },
    resetPasswordToken: {
      type: String,
      default: "",
    },
    resetPasswordExpire: {
      type: Date,
    },
    activationToken: {
      type: String,
      default: "",
    },
    activationTokenExpire: {
      type: Date,
    },
    activeOrganization: {
      type: mongoose.Schema.Types.ObjectId,
      ref: "Organization",
    },
    activeClickhouseCredentials: {
      type: mongoose.Schema.Types.ObjectId,
      ref: "ClickhouseCredentials",
    },
  },
  { timestamps: true }
);

module.exports = mongoose.model("User", userSchema);
