const { body, param, validationResult } = require("express-validator");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const Organization = require("../models/Organization");
const User = require("../models/User");
const errorResponse = require("../utils/errorResponse");
const slugify = require("../utils/slugify");

// Helper function to handle common validation and error responses
const handleValidation = (req, res, next) => {
  const errors = validationResult(req);
  if (!errors.isEmpty()) {
    return errorResponse(
      res,
      400,
      4005,
      "Validation errors",
      req.route.path,
      errors.array()
    );
  }
  next();
};

// Create ClickHouse credential
exports.createClickHouseCredential = [
  body("name").trim().notEmpty().withMessage("Name is required"),
  body("host")
    .trim()
    .notEmpty()
    .isURL()
    .withMessage("Valid host URL is required"),
  body("port")
    .optional()
    .isInt({ min: 1, max: 65535 })
    .withMessage("Valid port number is required"),
  body("username").trim().notEmpty().withMessage("Username is required"),
  body("password").trim().notEmpty().withMessage("Password is required"),
  body("users")
    .optional()
    .isArray()
    .withMessage("Users must be an array of user IDs"),
  body("users.*").isMongoId().withMessage("Invalid user ID"),
  body("allowedOrganizations")
    .isArray()
    .withMessage("Allowed organizations must be an array of organization IDs"),
  body("allowedOrganizations.*")
    .isMongoId()
    .withMessage("Invalid organization ID"),
  handleValidation,

  async (req, res) => {
    const {
      name,
      host,
      port,
      username,
      password,
      users = [],
      allowedOrganizations,
    } = req.body;

    try {
      const [organizationsCount, usersCount] = await Promise.all([
        Organization.countDocuments({ _id: { $in: allowedOrganizations } }),
        User.countDocuments({
          _id: { $in: [...new Set([...users, req.user._id.toString()])] },
        }),
      ]);

      if (organizationsCount !== allowedOrganizations.length) {
        return errorResponse(
          res,
          400,
          4002,
          "One or more organizations do not exist",
          "createClickHouseCredential"
        );
      }

      const uniqueUsers = [...new Set([...users, req.user._id.toString()])];
      if (usersCount !== uniqueUsers.length) {
        return errorResponse(
          res,
          400,
          4003,
          "One or more users do not exist",
          "createClickHouseCredential"
        );
      }

      const credential = new ClickHouseCredential({
        users: uniqueUsers,
        name,
        slug: slugify(name),
        host,
        port: port || 8123,
        username,
        password,
        allowedOrganizations,
        createdBy: req.user._id,
      });

      await credential.save();

      const responseCredential = credential.toObject();
      delete responseCredential.password;

      res.status(201).json({
        message: "ClickHouse credential created successfully",
        credential: responseCredential,
      });
    } catch (error) {
      console.error("Error in createClickHouseCredential:", error);
      errorResponse(
        res,
        500,
        4001,
        "Failed to create ClickHouse credential",
        "createClickHouseCredential"
      );
    }
  },
];

// Get ClickHouse credential by ID
exports.getClickHouseCredentialById = [
  param("id").isMongoId().withMessage("Invalid ClickHouse credential ID"),
  handleValidation,

  async (req, res) => {
    try {
      const credential = await ClickHouseCredential.findById(req.params.id)
        .populate("users", "id name email")
        .populate("allowedOrganizations", "id name slug")
        .select("-password");

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "getClickHouseCredentialById"
        );
      }

      res.json(credential);
    } catch (error) {
      console.error("Error in getClickHouseCredentialById:", error);
      errorResponse(
        res,
        500,
        4003,
        "Failed to fetch ClickHouse credential",
        "getClickHouseCredentialById"
      );
    }
  },
];

// Update ClickHouse credential
exports.updateClickHouseCredential = [
  param("id").isMongoId().withMessage("Invalid ClickHouse credential ID"),
  body("name").optional().trim().notEmpty().withMessage("Name cannot be empty"),
  body("host")
    .optional()
    .trim()
    .isURL()
    .withMessage("Valid host URL is required"),
  body("port")
    .optional()
    .isInt({ min: 1, max: 65535 })
    .withMessage("Valid port number is required"),
  body("username")
    .optional()
    .trim()
    .notEmpty()
    .withMessage("Username cannot be empty"),
  body("password")
    .optional()
    .trim()
    .notEmpty()
    .withMessage("Password cannot be empty"),
  body("users")
    .optional()
    .isArray()
    .withMessage("Users must be an array of user IDs"),
  body("users.*").isMongoId().withMessage("Invalid user ID"),
  body("allowedOrganizations")
    .optional()
    .isArray()
    .withMessage("Allowed organizations must be an array of organization IDs"),
  body("allowedOrganizations.*")
    .isMongoId()
    .withMessage("Invalid organization ID"),
  handleValidation,

  async (req, res) => {
    try {
      const credential = await ClickHouseCredential.findById(req.params.id);

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "updateClickHouseCredential"
        );
      }

      const updateFields = [
        "name",
        "host",
        "port",
        "username",
        "password",
        "users",
        "allowedOrganizations",
      ];
      updateFields.forEach((field) => {
        if (req.body[field] !== undefined) {
          credential[field] = req.body[field];
        }
      });

      if (req.body.name) {
        credential.slug = slugify(req.body.name);
      }

      await credential.save();

      const responseCredential = credential.toObject();
      delete responseCredential.password;

      res.json({
        message: "ClickHouse credential updated successfully",
        credential: responseCredential,
      });
    } catch (error) {
      console.error("Error in updateClickHouseCredential:", error);
      errorResponse(
        res,
        500,
        4004,
        "Failed to update ClickHouse credential",
        "updateClickHouseCredential"
      );
    }
  },
];

// Delete ClickHouse credential
exports.deleteClickHouseCredential = [
  param("id").isMongoId().withMessage("Invalid ClickHouse credential ID"),
  handleValidation,

  async (req, res) => {
    try {
      const credential = await ClickHouseCredential.findByIdAndDelete(
        req.params.id
      );

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "deleteClickHouseCredential"
        );
      }

      res.json({ message: "ClickHouse credential deleted successfully" });
    } catch (error) {
      console.error("Error in deleteClickHouseCredential:", error);
      errorResponse(
        res,
        500,
        4005,
        "Failed to delete ClickHouse credential",
        "deleteClickHouseCredential"
      );
    }
  },
];

// Assign ClickHouse credential to an organization
exports.assignCredentialToOrganization = [
  body("credentialId")
    .isMongoId()
    .withMessage("Invalid ClickHouse credential ID"),
  body("organizationId").isMongoId().withMessage("Invalid organization ID"),
  handleValidation,

  async (req, res) => {
    const { credentialId, organizationId } = req.body;

    try {
      const [credential, organization] = await Promise.all([
        ClickHouseCredential.findById(credentialId),
        Organization.findById(organizationId),
      ]);

      if (!credential || !organization) {
        return errorResponse(
          res,
          404,
          4006,
          "ClickHouse credential or organization not found",
          "assignCredentialToOrganization"
        );
      }

      if (!credential.allowedOrganizations.includes(organizationId)) {
        credential.allowedOrganizations.push(organizationId);
        await credential.save();
      }

      res.json({
        message: "ClickHouse credential assigned to organization successfully",
        credential: credential.toObject(),
      });
    } catch (error) {
      console.error("Error in assignCredentialToOrganization:", error);
      errorResponse(
        res,
        500,
        4006,
        "Failed to assign ClickHouse credential to organization",
        "assignCredentialToOrganization"
      );
    }
  },
];

// Revoke ClickHouse credential from an organization
exports.revokeCredentialFromOrganization = [
  body("credentialId")
    .isMongoId()
    .withMessage("Invalid ClickHouse credential ID"),
  body("organizationId").isMongoId().withMessage("Invalid organization ID"),
  handleValidation,

  async (req, res) => {
    const { credentialId, organizationId } = req.body;

    try {
      const credential = await ClickHouseCredential.findById(credentialId);

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "revokeCredentialFromOrganization"
        );
      }

      credential.allowedOrganizations = credential.allowedOrganizations.filter(
        (id) => id.toString() !== organizationId
      );

      await credential.save();

      res.json({
        message: "ClickHouse credential revoked from organization successfully",
        credential: credential.toObject(),
      });
    } catch (error) {
      console.error("Error in revokeCredentialFromOrganization:", error);
      errorResponse(
        res,
        500,
        4007,
        "Failed to revoke ClickHouse credential from organization",
        "revokeCredentialFromOrganization"
      );
    }
  },
];

// Assign user to ClickHouse credential
exports.assignUserToCredential = [
  body("credentialId")
    .isMongoId()
    .withMessage("Invalid ClickHouse credential ID"),
  body("userId").isMongoId().withMessage("Invalid user ID"),
  handleValidation,

  async (req, res) => {
    const { credentialId, userId } = req.body;

    try {
      const [credential, user] = await Promise.all([
        ClickHouseCredential.findById(credentialId),
        User.findById(userId),
      ]);

      if (!credential || !user) {
        return errorResponse(
          res,
          404,
          4008,
          "ClickHouse credential or user not found",
          "assignUserToCredential"
        );
      }

      const allowedOrganizations = await Organization.find({
        _id: { $in: credential.allowedOrganizations },
        members: userId,
      });

      if (allowedOrganizations.length === 0) {
        return errorResponse(
          res,
          403,
          4009,
          "User is not a member of any organization associated with this credential",
          "assignUserToCredential"
        );
      }

      if (!credential.users.includes(userId)) {
        credential.users.push(userId);
        await credential.save();
      }

      res.json({
        message: "User assigned to ClickHouse credential successfully",
        credential: credential.toObject(),
      });
    } catch (error) {
      console.error("Error in assignUserToCredential:", error);
      errorResponse(
        res,
        500,
        4008,
        "Failed to assign user to ClickHouse credential",
        "assignUserToCredential"
      );
    }
  },
];

// Revoke user from ClickHouse credential
exports.revokeUserFromCredential = [
  body("credentialId")
    .isMongoId()
    .withMessage("Invalid ClickHouse credential ID"),
  body("userId").isMongoId().withMessage("Invalid user ID"),
  handleValidation,

  async (req, res) => {
    const { credentialId, userId } = req.body;

    try {
      const credential = await ClickHouseCredential.findById(credentialId);

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "revokeUserFromCredential"
        );
      }

      credential.users = credential.users.filter(
        (id) => id.toString() !== userId
      );
      await credential.save();

      res.json({
        message: "User revoked from ClickHouse credential successfully",
        credential: credential.toObject(),
      });
    } catch (error) {
      console.error("Error in revokeUserFromCredential:", error);
      errorResponse(
        res,
        500,
        4009,
        "Failed to revoke user from ClickHouse credential",
        "revokeUserFromCredential"
      );
    }
  },
];

module.exports = exports;
