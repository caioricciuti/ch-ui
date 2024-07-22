const express = require("express");
const router = express.Router();

const {
  getUsers,
  getUserById,
  createUser,
  updateUser,
  deleteUser,
  setCurrentOrganizationForUser,
  getCurrentUser,
  updateUserRole,
  setCurrentCredentialsForUser,
} = require("../controllers/user.controller");

const isAuthenticated = require("../middleware/isAuthenticated");
const isAdmin = require("../middleware/isAdmin");
const isAuthorized = require("../middleware/isAuthorized");

// Public routes
// None in this router

// Protected routes
router.use(isAuthenticated);

// Current user routes
router.get("/me", getCurrentUser);
router.put("/me", isAuthorized("updateUser"), updateUser);
router.post("/me/organization", setCurrentOrganizationForUser);
router.post("/me/credential", setCurrentCredentialsForUser);

// Admin routes
router.get("/", isAdmin, getUsers);
router.post("/", isAdmin, createUser);
router.put("/role", isAdmin, updateUserRole);

// General user routes
router.get("/:id", getUserById);

// Sensitive operations
router.delete("/", isAuthorized("deleteUser"), deleteUser);

module.exports = router;
