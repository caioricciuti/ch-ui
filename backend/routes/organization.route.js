const express = require("express");
const router = express.Router();

const {
  getOrganizations,
  getOrganizationById,
  createOrganization,
  updateOrganization,
  deleteOrganization,
  addUserToOrganization,
  removeUserFromOrganization,
} = require("../controllers/organization.controller");

const isAuthenticated = require("../middleware/isAuthenticated");
const isAuthorized = require("../middleware/isAuthorized");

// Apply authentication middleware to all routes
router.use(isAuthenticated);

// Organization CRUD operations
router.get("/", getOrganizations);
router.get("/:id", getOrganizationById);
router.post("/", createOrganization);
router.put("/", isAuthorized("updateOrganization"), updateOrganization);
router.delete("/", isAuthorized("deleteOrganization"), deleteOrganization);

// Member management
router.post(
  "/:organizationId/members",
  isAuthorized("addMemberToOrganization"),
  addUserToOrganization
);
router.delete(
  "/:organizationId/members/:userId",
  isAuthorized("removeMemberFromOrganization"),
  removeUserFromOrganization
);

module.exports = router;
