const User = require("../models/User");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const { body, validationResult } = require("express-validator");
const mongoose = require("mongoose");
const errorResponse = require("../utils/errorResponse");

exports.getUsers = async (req, res) => {
  try {
    const users = await User.find({});
    res.json(users);
  } catch (error) {
    errorResponse(res, 500, 1001, "Failed to fetch users", "getUsers", error);
  }
};

exports.getUserById = async (req, res) => {
  try {
    if (!mongoose.Types.ObjectId.isValid(req.params.id)) {
      return errorResponse(res, 400, 1004, "Invalid user ID", "getUserById");
    }

    const user = await User.findById(req.params.id);
    if (!user) {
      return errorResponse(res, 404, 1002, "User not found", "getUserById");
    }
    res.json(user);
  } catch (error) {
    errorResponse(res, 500, 1003, "Failed to fetch user", "getUserById", error);
  }
};

exports.createUser = [
  body("name").isString().isLength({ max: 32 }).trim().notEmpty(),
  body("email").isEmail().trim().notEmpty(),
  body("password")
    .isString()
    .isLength({ min: 8 })
    .matches(/[A-Z]/)
    .withMessage("Password must contain at least one uppercase letter")
    .matches(/\W/)
    .withMessage("Password must contain at least one special character")
    .notEmpty(),
  body("role").optional().isIn(["admin", "user", "viewer"]),
  body("organization").optional().isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "createUser",
        errors
      );
    }

    try {
      const user = new User(req.body);
      await user.save();
      res.status(201).json(user);
    } catch (error) {
      errorResponse(
        res,
        500,
        1006,
        "Failed to create user",
        "createUser",
        error
      );
    }
  },
];

exports.getCurrentUser = (req, res) => {
  try {
    const user = req.user;
    res.json(user);
  } catch (error) {
    errorResponse(
      res,
      500,
      1018,
      "Failed to fetch current user",
      "getCurrentUser",
      error
    );
  }
};

exports.updateUser = [
  body("userId").isMongoId(),
  body("name").optional().isString().isLength({ max: 32 }).trim(),
  body("password")
    .optional()
    .isString()
    .isLength({ min: 8 })
    .matches(/[A-Z]/)
    .withMessage("Password must contain at least one uppercase letter")
    .matches(/\W/)
    .withMessage("Password must contain at least one special character"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "updateUser",
        errors
      );
    }

    try {
      const user = await User.findById(req.body.userId);
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "updateUser");
      }

      user.set(req.body);
      await user.save();
      res.json(user);
    } catch (error) {
      errorResponse(
        res,
        500,
        1007,
        "Failed to update user",
        "updateUser",
        error
      );
    }
  },
];

exports.deleteUser = [
  body("userId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "deleteUser",
        errors
      );
    }

    try {
      const user = await User.findByIdAndDelete(req.body.userId);
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "deleteUser");
      }
      res.json({ message: "User deleted successfully", user });
    } catch (error) {
      errorResponse(
        res,
        500,
        1008,
        "Failed to delete user",
        "deleteUser",
        error
      );
    }
  },
];

exports.setCurrentOrganizationForUser = [
  body("organizationId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "setCurrentOrganizationForUser",
        errors
      );
    }

    try {
      const user = req.user;
      if (!user) {
        return errorResponse(
          res,
          404,
          1002,
          "User not found",
          "setCurrentOrganizationForUser"
        );
      }

      const organization = await Organization.findById(req.body.organizationId);

      if (!organization) {
        return errorResponse(
          res,
          404,
          3002,
          "Organization not found",
          "setCurrentOrganizationForUser"
        );
      }

      // if user is not member of the organization return error
      if (!organization.members.includes(user._id.toString())) {
        return errorResponse(
          res,
          403,
          3002,
          "You are not a member of this organization",
          "setCurrentOrganizationForUser"
        );
      }

      user.activeOrganization = req.body.organizationId;
      await user.save();
      res.json({
        message: `${organization.name} is now set as current organization`,
      });
    } catch (error) {
      errorResponse(
        res,
        500,
        1019,
        "Failed to set current organization",
        "setCurrentOrganizationForUser",
        error
      );
    }
  },
];

exports.updateUserRole = [
  body("userId").isMongoId(),
  body("role").isIn(["admin", "user", "viewer"]),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "updateUserRole",
        errors
      );
    }

    try {
      const user = await User.findById(req.body.userId);
      if (!user) {
        return errorResponse(
          res,
          404,
          1002,
          "User not found",
          "updateUserRole"
        );
      }

      user.role = req.body.role;
      await user.save();
      res.json({ message: "User role updated successfully" });
    } catch (error) {
      errorResponse(
        res,
        500,
        1020,
        "Failed to update user role",
        "updateUserRole",
        error
      );
    }
  },
];

exports.setCurrentCredentialsForUser = [
  body("credentialId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "setCurrentCredentialsForUser",
        errors
      );
    }

    try {
      const user = req.user;
      if (!user) {
        return errorResponse(
          res,
          404,
          1002,
          "User not found",
          "setCurrentCredentialsForUser"
        );
      }

      const credentials = await ClickHouseCredential.findById(
        req.body.credentialId
      );

      if (!credentials) {
        return errorResponse(
          res,
          404,
          6003,
          "Credential not found",
          "setCurrentCredentialsForUser"
        );
      }

      // if user is not allowed to use the credentials return error
      if (!credentials.users.includes(user._id.toString())) {
        return errorResponse(
          res,
          403,
          6004,
          "You are not allowed to use these credentials",
          "setCurrentCredentialsForUser"
        );
      }

      user.activeClickhouseCredential = req.body.credentialId;
      await user.save();
      res.json({
        message: `Credentials are now set as current credentials`,
      });
    } catch (error) {
      errorResponse(
        res,
        500,
        1021,
        "Failed to set current credentials",
        "setCurrentCredentialsForUser",
        error
      );
    }
  },
];
