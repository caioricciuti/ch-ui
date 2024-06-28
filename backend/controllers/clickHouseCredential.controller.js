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
  body("name").notEmpty().withMessage("Name is required"),
  body("host").notEmpty().isURL().withMessage("Valid host URL is required"),
  body("username").notEmpty().withMessage("Username is required"),
  body("password").notEmpty().withMessage("Password is required"),
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
      username,
      password,
      users = [],
      allowedOrganizations,
    } = req.body;

    try {
      // Check if organizations exist
      const organizationsExist = await Organization.countDocuments({
        _id: { $in: allowedOrganizations },
      });
      if (organizationsExist !== allowedOrganizations.length) {
        return errorResponse(
          res,
          400,
          4002,
          "One or more organizations do not exist",
          "createClickHouseCredential"
        );
      }

      // Check if users exist (excluding req.user)
      const uniqueUsers = [...new Set([...users, req.user._id.toString()])];
      const usersExist = await User.countDocuments({
        _id: { $in: uniqueUsers },
      });
      if (usersExist !== uniqueUsers.length) {
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
        username,
        password,
        allowedOrganizations,
        createdBy: req.user._id,
      });

      await credential.save();

      // Remove sensitive information from the response
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
        "createClickHouseCredential",
        error.message
      );
    }
  },
];

// Get ClickHouse credential by ID
exports.getClickHouseCredentialById = [
  param("id").isMongoId().withMessage("Invalid ClickHouse credential ID"),
  handleValidation,

  async (req, res) => {
    const { id } = req.params;

    try {
      const credential = await ClickHouseCredential.findById(id)
        .populate("users", "id name email")
        .populate("allowedOrganizations", "id name slug");

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
        "getClickHouseCredentialById",
        error.message
      );
    }
  },
];

// Update ClickHouse credential
exports.updateClickHouseCredential = [
  param("id").isMongoId().withMessage("Invalid ClickHouse credential ID"),
  body("name").optional().notEmpty().withMessage("Name cannot be empty"),
  body("host").optional().isURL().withMessage("Valid host URL is required"),
  body("username")
    .optional()
    .notEmpty()
    .withMessage("Username cannot be empty"),
  body("password")
    .optional()
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
    const { id } = req.params;
    const updateFields = [
      "name",
      "host",
      "username",
      "password",
      "users",
      "allowedOrganizations",
    ];

    try {
      const credential = await ClickHouseCredential.findById(id);

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "updateClickHouseCredential"
        );
      }

      // Update only the fields that are present in the request body
      updateFields.forEach((field) => {
        if (req.body[field] !== undefined) {
          credential[field] = req.body[field];
        }
      });

      credential.slug = slugify(credential.name);

      await credential.save();

      // Remove sensitive information from the response
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
        "updateClickHouseCredential",
        error.message
      );
    }
  },
];

// Delete ClickHouse credential
exports.deleteClickHouseCredential = [
  param("id").isMongoId().withMessage("Invalid ClickHouse credential ID"),
  handleValidation,

  async (req, res) => {
    const { id } = req.params;

    try {
      const credential = await ClickHouseCredential.findByIdAndDelete(id);

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
        "deleteClickHouseCredential",
        error.message
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

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "assignCredentialToOrganization"
        );
      }

      if (!organization) {
        return errorResponse(
          res,
          404,
          4006,
          "Organization not found",
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
        "assignCredentialToOrganization",
        error.message
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
        "revokeCredentialFromOrganization",
        error.message
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

      if (!credential) {
        return errorResponse(
          res,
          404,
          4002,
          "ClickHouse credential not found",
          "assignUserToCredential"
        );
      }

      if (!user) {
        return errorResponse(
          res,
          404,
          4008,
          "User not found",
          "assignUserToCredential"
        );
      }

      // Fetch all allowed organizations for this credential
      const allowedOrganizations = await Organization.find({
        _id: { $in: credential.allowedOrganizations },
      });

      // Check if the user is a member of any of the allowed organizations
      const isUserMemberOfAllowedOrg = allowedOrganizations.some((org) =>
        org.members.includes(userId)
      );

      if (!isUserMemberOfAllowedOrg) {
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
        "assignUserToCredential",
        error.message
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
        "revokeUserFromCredential",
        error.message
      );
    }
  },
];
