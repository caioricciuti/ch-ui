const User = require("../models/User");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const Editor = require("../models/Editor");
const Metrics = require("../models/Metrics");
const errorResponse = require("../utils/errorResponse");

const isAuthorized = (action) => {
  return async (req, res, next) => {
    try {
      const user = await User.findById(req.user.id);
      let organization;
      let clickHouseCredential;
      let editor;
      let metrics;

      if (!user) {
        return errorResponse(res, 401, 3001, "User not found", "isAuthorized");
      }

      // Check if the user is an admin
      /*
      if (user.role === "admin") {
        return next();
      }
        */

      if (
        action === "updateOrganization" ||
        action === "deleteOrganization" ||
        action === "AddMemberToOrganization" ||
        action === "RemoveMemberFromOrganization"
      ) {
        organization = await Organization.findById(req.body.organizationId);
        if (!organization) {
          return errorResponse(
            res,
            404,
            3002,
            "Organization not found",
            "isAuthorized"
          );
        }
      }

      // Define permissions
      const permissions = {
        // User permissions
        updateUser: user._id.toString() === req.body.userId,
        deleteUser: user._id.toString() === req.body.userId,

        // Organization permissions
        updateOrganization:
          user._id.toString() === organization.owner._id.toString(),
        deleteOrganization:
          user._id.toString() === organization.owner._id.toString(),
        AddMemberToOrganization:
          user._id.toString() === organization.owner._id.toString(),
        RemoveMemberFromOrganization:
          user._id.toString() === organization.owner._id.toString(),
      };

      // Check if the user is authorized to perform the action
      if (permissions[action]) {
        return next();
      }

      return errorResponse(
        res,
        403,
        3002,
        `Sorry, ${user.name}, you don't have permissions to ${action}`,
        "isAuthorized"
      );
    } catch (error) {
      errorResponse(
        res,
        500,
        3003,
        "Internal server error",
        "isAuthorized",
        error
      );
    }
  };
};

module.exports = isAuthorized;
