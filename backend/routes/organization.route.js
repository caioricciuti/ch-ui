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

// Public routes
router.get("/organizations", getOrganizations);
router.get("/organizations/:id", getOrganizationById);

// Protected routes
router.post("/organizations", isAuthenticated, createOrganization);

router.put("/organizations", isAuthenticated, updateOrganization);

router.delete("/organizations", isAuthenticated, deleteOrganization);

router.post("/organizations/addUser", isAuthenticated, addUserToOrganization);

router.post(
  "/organizations/removeUser",
  isAuthenticated,
  removeUserFromOrganization
);

module.exports = router;
