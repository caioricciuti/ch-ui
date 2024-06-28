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

// Middleware for checking JWT and Authorization
const isAuthenticated = require("../middleware/isAuthenticated");
const isAdmin = require("../middleware/isAdmin");
const isAuthorized = require("../middleware/isAuthorized");

router.get("/", isAuthenticated, isAdmin, getUsers);
router.get("/me", isAuthenticated, getCurrentUser);
router.put("/update/role", isAuthenticated, isAdmin, updateUserRole);
router.get("/:id", isAuthenticated, getUserById);
router.post("/", isAuthenticated, isAdmin, createUser);
router.put("/", isAuthenticated, isAuthorized("updateUser"), updateUser);
router.post(
  "/set-current-organization",
  isAuthenticated,
  setCurrentOrganizationForUser
);

router.post(
  "/set-current-credential",
  isAuthenticated,
  setCurrentCredentialsForUser
);

router.delete(
  "/users",
  isAuthenticated,
  isAuthorized("deleteUser"),
  deleteUser
);

module.exports = router;
