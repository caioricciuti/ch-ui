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

      let organization;

      // Fetch organization data for all actions
      const organizationId =
        req.body.organizationId || req.params.organizationId;
      if (organizationId) {
        organization = await Organization.findById(organizationId).select(
          "owner members"
        );
        if (!organization) {
          return errorResponse(
            res,
            404,
            3003,
            "Organization not found",
            "isAuthorized"
          );
        }
      }

      const payloadUserId = req.body.userId;
      const requestedUserId = req.user.id;

      // Define permissions
      const permissions = {
        updateUser: () => requestedUserId === payloadUserId,
        deleteUser: () => requestedUserId === payloadUserId,
        updateOrganization: () => isOwnerOrMember(user, organization),
        deleteOrganization: () => isOwner(user, organization),
        addMemberToOrganization: () => isOwner(user, organization),
        removeMemberFromOrganization: () => isOwner(user, organization),
        createClickHouseCredential: () => isOwnerOrMember(user, organization),
        updateClickHouseCredential: () => isOwnerOrMember(user, organization),
        deleteClickHouseCredential: () => isOwnerOrMember(user, organization),
        createEditor: () => isOwnerOrMember(user, organization),
        updateEditor: () => isOwnerOrMember(user, organization),
        deleteEditor: () => isOwnerOrMember(user, organization),
        createMetrics: () => isOwnerOrMember(user, organization),
        updateMetrics: () => isOwnerOrMember(user, organization),
        deleteMetrics: () => isOwnerOrMember(user, organization),
        // Add more permission checks as needed
      };

      // Check if the user is authorized to perform the action
      if (permissions[action] && permissions[action]()) {
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

// Helper functions
const isOwner = (user, organization) => {
  return organization && user._id.toString() === organization.owner.toString();
};

const isOwnerOrMember = (user, organization) => {
  return (
    organization &&
    (isOwner(user, organization) ||
      organization.members.some(
        (member) => member.toString() === user._id.toString()
      ))
  );
};

module.exports = isAuthorized;
