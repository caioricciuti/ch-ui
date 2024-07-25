const User = require("../models/User");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const { body, param, validationResult } = require("express-validator");
const mongoose = require("mongoose");
const errorResponse = require("../utils/errorResponse");

const validateObjectId = (id) => mongoose.Types.ObjectId.isValid(id);

exports.getUsers = async (req, res) => {
  try {
    const users = await User.find({}).select("-password");
    res.json(users);
  } catch (error) {
    console.error("getUsers error:", error);
    errorResponse(res, 500, 1001, "Failed to fetch users", "getUsers");
  }
};

exports.getUserById = [
  param("id").custom(validateObjectId).withMessage("Invalid user ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1004,
        "Invalid user ID",
        "getUserById",
        errors.array()
      );
    }

    try {
      const user = await User.findById(req.params.id).select("-password");
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "getUserById");
      }
      res.json(user);
    } catch (error) {
      console.error("getUserById error:", error);
      errorResponse(res, 500, 1003, "Failed to fetch user", "getUserById");
    }
  },
];

exports.createUser = [
  body("name")
    .trim()
    .isLength({ min: 2, max: 32 })
    .withMessage("Name must be between 2 and 32 characters"),
  body("email").isEmail().normalizeEmail(),
  body("password")
    .isLength({ min: 8 })
    .matches(/^(?=.*[A-Z])(?=.*\d)(?=.*[@$!%*?&])[A-Za-z\d@$!%*?&]{8,}$/)
    .withMessage(
      "Password must be at least 8 characters long, contain an uppercase letter, a number, and a special character"
    ),
  body("role").optional().isIn(["admin", "user", "viewer"]),
  body("organization")
    .optional()
    .custom(validateObjectId)
    .withMessage("Invalid organization ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "createUser",
        errors.array()
      );
    }

    try {
      const { email } = req.body;
      const existingUser = await User.findOne({ email });
      if (existingUser) {
        return errorResponse(
          res,
          409,
          1023,
          "Email already in use",
          "createUser"
        );
      }

      const user = new User(req.body);
      await user.save();

      const userResponse = user.toObject();
      delete userResponse.password;

      res.status(201).json(userResponse);
    } catch (error) {
      console.error("createUser error:", error);
      errorResponse(res, 500, 1006, "Failed to create user", "createUser");
    }
  },
];

exports.getCurrentUser = (req, res) => {
  try {
    const user = req.user.toObject();
    delete user.password;
    res.json(user);
  } catch (error) {
    console.error("getCurrentUser error:", error);
    errorResponse(
      res,
      500,
      1018,
      "Failed to fetch current user",
      "getCurrentUser"
    );
  }
};

exports.updateUser = [
  body("userId").custom(validateObjectId).withMessage("Invalid user ID"),
  body("name")
    .optional()
    .trim()
    .isLength({ min: 2, max: 32 })
    .withMessage("Name must be between 2 and 32 characters"),
  body("password")
    .optional()
    .isLength({ min: 8 })
    .matches(/^(?=.*[A-Z])(?=.*\d)(?=.*[@$!%*?&])[A-Za-z\d@$!%*?&]{8,}$/)
    .withMessage(
      "Password must be at least 8 characters long, contain an uppercase letter, a number, and a special character"
    ),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "updateUser",
        errors.array()
      );
    }

    try {
      const user = await User.findById(req.body.userId);
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "updateUser");
      }

      const updates = { ...req.body };
      delete updates.userId;
      delete updates.email; // Prevent email updates through this route

      user.set(updates);
      await user.save();

      const userResponse = user.toObject();
      delete userResponse.password;

      res.json(userResponse);
    } catch (error) {
      console.error("updateUser error:", error);
      errorResponse(res, 500, 1007, "Failed to update user", "updateUser");
    }
  },
];

exports.deleteUser = [
  body("userId").custom(validateObjectId).withMessage("Invalid user ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "deleteUser",
        errors.array()
      );
    }

    try {
      const user = await User.findByIdAndDelete(req.body.userId);
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "deleteUser");
      }
      res.json({ message: "User deleted successfully", userId: user._id });
    } catch (error) {
      console.error("deleteUser error:", error);
      errorResponse(res, 500, 1008, "Failed to delete user", "deleteUser");
    }
  },
];

exports.setCurrentOrganizationForUser = [
  body("organizationId")
    .custom(validateObjectId)
    .withMessage("Invalid organization ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "setCurrentOrganizationForUser",
        errors.array()
      );
    }

    try {
      const user = req.user;
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

      if (!organization.members.includes(user._id)) {
        return errorResponse(
          res,
          403,
          3003,
          "You are not a member of this organization",
          "setCurrentOrganizationForUser"
        );
      }

      user.activeOrganization = req.body.organizationId;
      await user.save();
      res.json({
        message: `${organization.name} is now set as current organization`,
        organizationId: organization._id,
      });
    } catch (error) {
      console.error("setCurrentOrganizationForUser error:", error);
      errorResponse(
        res,
        500,
        1019,
        "Failed to set current organization",
        "setCurrentOrganizationForUser"
      );
    }
  },
];

exports.updateUserRole = [
  body("userId").custom(validateObjectId).withMessage("Invalid user ID"),
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
        errors.array()
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
      res.json({
        message: "User role updated successfully",
        userId: user._id,
        newRole: user.role,
      });
    } catch (error) {
      console.error("updateUserRole error:", error);
      errorResponse(
        res,
        500,
        1020,
        "Failed to update user role",
        "updateUserRole"
      );
    }
  },
];

exports.setCurrentCredentialsForUser = [
  body("credentialId")
    .custom(validateObjectId)
    .withMessage("Invalid credential ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "setCurrentCredentialsForUser",
        errors.array()
      );
    }

    try {
      const user = req.user;
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

      if (!credentials.users.includes(user._id)) {
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
        message: "Credentials are now set as current credentials",
        credentialId: credentials._id,
      });
    } catch (error) {
      console.error("setCurrentCredentialsForUser error:", error);
      errorResponse(
        res,
        500,
        1021,
        "Failed to set current credentials",
        "setCurrentCredentialsForUser"
      );
    }
  },
];

module.exports = exports;
