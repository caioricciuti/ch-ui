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

// Middleware for checking JWT
const isAuthenticated = require("../middleware/isAuthenticated");
const isAuthorized = require("../middleware/isAuthorized");

// Public routes
router.get("/", isAuthenticated, getOrganizations);
router.get("/:id", isAuthenticated, getOrganizationById);

// Protected routes
router.post("/", isAuthenticated, createOrganization);

router.put(
  "/",
  isAuthenticated,
  isAuthorized("updateOrganization"),
  updateOrganization
);

router.delete(
  "/",
  isAuthenticated,
  isAuthorized("deleteOrganization"),
  deleteOrganization
);

router.post(
  "/add-member",
  isAuthenticated,
  isAuthorized("AddMemberToOrganization"),
  addUserToOrganization
);

router.post(
  "/remove-member",
  isAuthenticated,
  isAuthorized("RemoveMemberFromOrganization"),
  removeUserFromOrganization
);

module.exports = router;
