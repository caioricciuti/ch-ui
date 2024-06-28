const Organization = require("../models/Organization");
const User = require("../models/User");
const { body, validationResult } = require("express-validator");
const mongoose = require("mongoose");
const errorResponse = require("../utils/errorResponse");
const slugify = require("../utils/slugify");

// get all organizations
exports.getOrganizations = async (req, res) => {
  try {
    const organizations = await Organization.find({
      // where the user is a member
      members: req.user.id,
    })
      .populate({
        path: "members",
        select: "id name email",
      })
      .populate({
        path: "owner",
        select: "id name email",
      });
    res.json(organizations);
  } catch (error) {
    errorResponse(
      res,
      500,
      2001,
      "Failed to fetch organizations",
      "getOrganizations",
      error
    );
  }
};

// get organization by id
exports.getOrganizationById = async (req, res) => {
  try {
    const organizationId = req.params.id;

    // Validate the ObjectId format
    if (!mongoose.Types.ObjectId.isValid(organizationId)) {
      return errorResponse(
        res,
        400,
        2004,
        "Invalid organization ID",
        "getOrganizationById"
      );
    }

    // Fetch the organization with populated members and owner
    const organization = await Organization.findById(organizationId)
      .populate({
        path: "members", // Populate the members field
        // only select id, name, and email
        select: "id name email",
      })
      .populate({
        path: "owner", // Populate the owner field
        // only select id, name, and email
        select: "id name email",
      });

    if (!organization) {
      return errorResponse(
        res,
        404,
        2002,
        "Organization not found",
        "getOrganizationById"
      );
    }

    // if user is not a member of the organization return 403
    if (!organization.members.includes(req.user.id)) {
      organizationOwner = organization.owner?.name || "Unknown";
      return errorResponse(
        res,
        403,
        2005,
        `This organization belongs to ${organizationOwner}, contact him/her to get access`,
        "getOrganizationById"
      );
    }

    res.json(organization);
  } catch (error) {
    errorResponse(
      res,
      500,
      2003,
      "Failed to fetch organization",
      "getOrganizationById",
      error
    );
  }
};

// create organization
exports.createOrganization = [
  body("name").isString().isLength({ max: 32 }).trim().notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        `Validation errors, ${JSON.stringify(errors.array())}`,
        "createOrganization"
      );
    }

    try {
      // check if organization with the same slug exists
      const organizationExists = await Organization.findOne({
        slug: slugify(req.body.name),
      });
      if (organizationExists) {
        return errorResponse(
          res,
          400,
          2005,
          "Organization with the same name already exists",
          "createOrganization"
        );
      }

      let organization = new Organization(req.body);
      organization.owner = req.user.id;
      organization.slug = slugify(req.body.name);
      organization.members.push(req.user.id);
      await organization.save();
      res.status(201).json(organization);
    } catch (error) {
      errorResponse(
        res,
        500,
        2006,
        "Failed to create organization",
        "createOrganization",
        error
      );
    }
  },
];

// update organization
exports.updateOrganization = [
  body("organizationId").isMongoId(),
  body("name").optional().isString().isLength({ max: 32 }).trim(),
  body("owner").optional().isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "updateOrganization",
        errors
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

      organization.set({
        name: req.body.name,
        slug: slugify(req.body.name),
      });
      await organization.save();
      res.json(organization);
    } catch (error) {
      errorResponse(
        res,
        500,
        2007,
        "Failed to update organization",
        "updateOrganization",
        error
      );
    }
  },
];

// delete organization
exports.deleteOrganization = [
  body("organizationId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "deleteOrganization",
        errors
      );
    }

    try {
      const organization = await Organization.findByIdAndDelete(
        req.body.organizationId
      );
      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "deleteOrganization"
        );
      }
      res.json({ message: "Organization deleted successfully", organization });
    } catch (error) {
      errorResponse(
        res,
        500,
        2008,
        "Failed to delete organization",
        "deleteOrganization",
        error
      );
    }
  },
];

// add user to organization
exports.addUserToOrganization = [
  body("organizationId").isMongoId().withMessage("Invalid organization ID"),
  body("userId").isMongoId().withMessage("Invalid user ID"),

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

    const { organizationId, userId } = req.body;

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
          "User with the given ID not found",
          "addUserToOrganization"
        );
      }

      if (organization.members.includes(userId)) {
        return errorResponse(
          res,
          400,
          2005,
          "User already exists in the organization",
          "addUserToOrganization"
        );
      }

      organization.members.push(userId);
      await organization.save();

      res.status(200).json({
        message: `${userToAdd.name} added to organization ${organization.name} successfully`,
        organization: {
          id: organization._id,
          name: organization.name,
          memberCount: organization.members.length,
        },
      });
    } catch (error) {
      console.error("Error in addUserToOrganization:", error);
      errorResponse(
        res,
        500,
        2009,
        "Failed to add user to organization",
        "addUserToOrganization",
        error.message
      );
    }
  },
];

// remove user from organization
exports.removeUserFromOrganization = [
  body("organizationId").isMongoId().withMessage("Invalid organization ID"),
  body("userId").isMongoId().withMessage("Invalid user ID"),

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

    const { organizationId, userId } = req.body;

    try {
      const [organization, userToRemove] = await Promise.all([
        Organization.findById(organizationId),
        User.findById(userId),
      ]);

      if (!organization) {
        return errorResponse(
          res,
          404,
          2002,
          "Organization not found",
          "removeUserFromOrganization"
        );
      }

      if (!userToRemove) {
        return errorResponse(
          res,
          404,
          1002,
          "User not found",
          "removeUserFromOrganization"
        );
      }

      if (!organization.members.includes(userId)) {
        return errorResponse(
          res,
          400,
          2006,
          "User is not a member of this organization",
          "removeUserFromOrganization"
        );
      }

      organization.members = organization.members.filter(
        (memberId) => memberId.toString() !== userId
      );
      await organization.save();

      res.status(200).json({
        message: "User successfully removed from the organization",
        organization: {
          id: organization._id,
          name: organization.name,
          memberCount: organization.members.length,
        },
      });
    } catch (error) {
      console.error("Error in removeUserFromOrganization:", error);
      errorResponse(
        res,
        500,
        2010,
        "Failed to remove user from organization",
        "removeUserFromOrganization",
        error.message
      );
    }
  },
];
