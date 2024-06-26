const Organization = require("../models/Organization");
const { body, validationResult } = require("express-validator");
const mongoose = require("mongoose");
const errorResponse = require("../utils/errorResponse");
const slugify = require("../utils/slugify");
// get all organizations
exports.getOrganizations = async (req, res) => {
  try {
    const organizations = await Organization.find({})
      .populate({
        path: "members", // Populate the members field
        // only select id, name, and email
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
  body("slug").optional().isString().trim(),
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

      organization.set(req.body);
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
  body("organizationId").isMongoId(),
  body("userId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "addUserToOrganization",
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
          "addUserToOrganization"
        );
      }

      organization.members.push(req.body.userId);
      await organization.save();
      res.json(organization);
    } catch (error) {
      errorResponse(
        res,
        500,
        2009,
        "Failed to add user to organization",
        "addUserToOrganization",
        error
      );
    }
  },
];

// remove user from organization
exports.removeUserFromOrganization = [
  body("organizationId").isMongoId(),
  body("userId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2005,
        "Validation errors",
        "removeUserFromOrganization",
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
          "removeUserFromOrganization"
        );
      }

      organization.members.pull(req.body.userId);
      await organization.save();
      res.json(organization);
    } catch (error) {
      errorResponse(
        res,
        500,
        2010,
        "Failed to remove user from organization",
        "removeUserFromOrganization",
        error
      );
    }
  },
];
