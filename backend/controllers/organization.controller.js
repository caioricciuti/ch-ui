const Organization = require("../models/Organization");
const User = require("../models/User");
const { body, param, validationResult } = require("express-validator");
const mongoose = require("mongoose");
const errorResponse = require("../utils/errorResponse");
const slugify = require("../utils/slugify");

const validateObjectId = (value) => mongoose.Types.ObjectId.isValid(value);

const isAdminOrOwner = (user, organization) => {
  return (
    user.role === "admin" ||
    (organization && organization.owner.equals(user.id))
  );
};

exports.getOrganizations = async (req, res) => {
  try {
    const organizations = await Organization.find({ members: req.user.id })
      .populate("members", "id name email")
      .populate("owner", "id name email");
    res.json(organizations);
  } catch (error) {
    console.error("getOrganizations error:", error);
    errorResponse(
      res,
      500,
      2001,
      "Failed to fetch organizations",
      "getOrganizations"
    );
  }
};

exports.getOrganizationById = [
  param("id").custom(validateObjectId).withMessage("Invalid organization ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2004,
        "Invalid organization ID",
        "getOrganizationById",
        errors.array()
      );
    }

    try {
      const organization = await Organization.findById(req.params.id)
        .populate("members", "id name email")
        .populate("owner", "id name email");

      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "getOrganizationById"
        );
      }

      if (
        !organization.members.some((member) =>
          member._id.equals(req.user.id)
        ) &&
        req.user.role !== "admin"
      ) {
        return errorResponse(
          res,
          403,
          2005,
          "Access denied",
          "getOrganizationById"
        );
      }

      res.json(organization);
    } catch (error) {
      console.error("getOrganizationById error:", error);
      errorResponse(
        res,
        500,
        2003,
        "Failed to fetch organization",
        "getOrganizationById"
      );
    }
  },
];

exports.createOrganization = [
  body("name")
    .trim()
    .isLength({ min: 2, max: 32 })
    .withMessage("Name must be between 2 and 32 characters"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "createOrganization",
        errors.array()
      );
    }

    try {
      const slug = slugify(req.body.name);
      const organizationExists = await Organization.findOne({ slug });
      if (organizationExists) {
        return errorResponse(
          res,
          409,
          2005,
          "Organization with the same name already exists",
          "createOrganization"
        );
      }

      const organization = new Organization({
        name: req.body.name,
        slug,
        owner: req.user.id,
        members: [req.user.id],
      });

      await organization.save();
      res.status(201).json(organization);
    } catch (error) {
      console.error("createOrganization error:", error);
      errorResponse(
        res,
        500,
        2006,
        "Failed to create organization",
        "createOrganization"
      );
    }
  },
];

exports.updateOrganization = [
  body("organizationId")
    .custom(validateObjectId)
    .withMessage("Invalid organization ID"),
  body("name")
    .optional()
    .trim()
    .isLength({ min: 2, max: 32 })
    .withMessage("Name must be between 2 and 32 characters"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "updateOrganization",
        errors.array()
      );
    }

    try {
      const organization = await Organization.findById(req.body.organizationId);
      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "updateOrganization"
        );
      }

      if (!organization.owner.equals(req.user.id)) {
        return errorResponse(
          res,
          403,
          2007,
          "Only the owner can update the organization",
          "updateOrganization"
        );
      }

      if (req.body.name) {
        organization.name = req.body.name;
        organization.slug = slugify(req.body.name);
      }

      await organization.save();
      res.json(organization);
    } catch (error) {
      console.error("updateOrganization error:", error);
      errorResponse(
        res,
        500,
        2007,
        "Failed to update organization",
        "updateOrganization"
      );
    }
  },
];

exports.deleteOrganization = [
  body("organizationId")
    .custom(validateObjectId)
    .withMessage("Invalid organization ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "deleteOrganization",
        errors.array()
      );
    }

    try {
      const organization = await Organization.findById(req.body.organizationId);
      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "deleteOrganization"
        );
      }

      if (!organization.owner.equals(req.user.id)) {
        return errorResponse(
          res,
          403,
          2008,
          "Only the owner can delete the organization",
          "deleteOrganization"
        );
      }

      await Organization.findByIdAndDelete(req.body.organizationId);
      res.json({
        message: "Organization deleted successfully",
        organizationId: organization._id,
      });
    } catch (error) {
      console.error("deleteOrganization error:", error);
      errorResponse(
        res,
        500,
        2008,
        "Failed to delete organization",
        "deleteOrganization"
      );
    }
  },
];

exports.addUserToOrganization = [
  body("userId").custom(validateObjectId).withMessage("Invalid user ID"),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "addUserToOrganization",
        errors.array()
      );
    }

    const { organizationId } = req.params;
    const { userId } = req.body;

    try {
      const [organization, userToAdd] = await Promise.all([
        Organization.findById(organizationId),
        User.findById(userId),
      ]);

      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "addUserToOrganization"
        );
      }

      if (!userToAdd) {
        return errorResponse(
          res,
          404,
          1002,
          "User not found",
          "addUserToOrganization"
        );
      }

      if (!isAdminOrOwner(req.user, organization)) {
        return errorResponse(
          res,
          403,
          2009,
          "Only the owner or admin can add members to the organization",
          "addUserToOrganization"
        );
      }

      if (organization.members.includes(userId)) {
        return errorResponse(
          res,
          409,
          2010,
          "User is already a member of this organization",
          "addUserToOrganization"
        );
      }

      organization.members.push(userId);
      await organization.save();

      res.json({
        message: `${userToAdd.name} added to organization ${organization.name} successfully`,
        organization: {
          id: organization._id,
          name: organization.name,
          memberCount: organization.members.length,
        },
      });
    } catch (error) {
      console.error("addUserToOrganization error:", error);
      errorResponse(
        res,
        500,
        2009,
        "Failed to add user to organization",
        "addUserToOrganization"
      );
    }
  },
];

exports.removeUserFromOrganization = [
  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "removeUserFromOrganization",
        errors.array()
      );
    }

    const organizationId = req.params.organizationId;
    const userId = req.params.userId;

    try {
      const organization = await Organization.findById(organizationId);

      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "removeUserFromOrganization"
        );
      }

      if (!isAdminOrOwner(req.user, organization)) {
        return errorResponse(
          res,
          403,
          2011,
          "Only the owner or admin can remove members from the organization",
          "removeUserFromOrganization"
        );
      }

      if (organization.owner.equals(userId) && req.user.role !== "admin") {
        return errorResponse(
          res,
          403,
          2012,
          "Cannot remove the owner from the organization",
          "removeUserFromOrganization"
        );
      }

      if (!organization.members.includes(userId)) {
        return errorResponse(
          res,
          404,
          2013,
          "User is not a member of this organization",
          "removeUserFromOrganization"
        );
      }

      organization.members = organization.members.filter(
        (memberId) => !memberId.equals(userId)
      );
      await organization.save();

      res.json({
        message: "User successfully removed from the organization",
        organization: {
          id: organization._id,
          name: organization.name,
          memberCount: organization.members.length,
        },
      });
    } catch (error) {
      console.error("removeUserFromOrganization error:", error);
      errorResponse(
        res,
        500,
        2010,
        "Failed to remove user from organization",
        "removeUserFromOrganization"
      );
    }
  },
];

module.exports = exports;
