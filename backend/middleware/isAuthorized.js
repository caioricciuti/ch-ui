const User = require("../models/User");
const Organization = require("../models/Organization");
const ClickHouseCredential = require("../models/ClickHouseCredential");
const Editor = require("../models/Editor");
const Metrics = require("../models/Metrics");
const errorResponse = require("../utils/errorResponse");

const isAuthorized = (action) => {
  return async (req, res, next) => {
    try {
      if (!req.user) {
        return errorResponse(
          res,
          401,
          3001,
          "User not authenticated",
          "isAuthorized"
        );
      }

      const user = await User.findById(req.user.id).select("+role");
      if (!user) {
        return errorResponse(res, 404, 3002, "User not found", "isAuthorized");
      }

      if (user.role === "admin") {
        return next();
      }

      let organization, clickHouseCredential, editor, metrics;

      // Fetch necessary data based on the action
      switch (action) {
        case "updateOrganization":
        case "deleteOrganization":
        case "addMemberToOrganization":
        case "removeMemberFromOrganization":
          organization = await Organization.findById(
            req.body.organizationId
          ).select("owner");
          if (!organization) {
            return errorResponse(
              res,
              404,
              3003,
              "Organization not found",
              "isAuthorized"
            );
          }
          break;
        // Add more cases for other actions that require fetching data
      }

      // Define permissions
      const permissions = {
        updateUser: (reqUserId) => user._id.toString() === reqUserId,
        deleteUser: (reqUserId) => user._id.toString() === reqUserId,
        updateOrganization: () =>
          user._id.toString() === organization.owner.toString(),
        deleteOrganization: () =>
          user._id.toString() === organization.owner.toString(),
        addMemberToOrganization: () =>
          user._id.toString() === organization.owner.toString(),
        removeMemberFromOrganization: () =>
          user._id.toString() === organization.owner.toString(),
        // Add more permission checks as needed
      };

      // Check if the user is authorized to perform the action
      if (permissions[action] && permissions[action](req.body.userId)) {
        return next();
      }

      return errorResponse(
        res,
        403,
        3004,
        `You don't have permission to perform this action`,
        "isAuthorized"
      );
    } catch (error) {
      console.error("isAuthorized middleware error:", error);
      return errorResponse(
        res,
        500,
        3005,
        "Internal server error",
        "isAuthorized"
      );
    }
  };
};

module.exports = isAuthorized;
