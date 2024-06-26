const express = require("express");
const router = express.Router();

const {
  getUsers,
  getUserById,
  createUser,
  updateUser,
  deleteUser,
  login,
  register,
  activateAccount,
  forgotPassword,
  resetPassword,
  refreshToken,
  logout,
  setCurrentOrganizationForUser,
} = require("../controllers/user.controller");

// Middleware for checking JWT and Authorization
const isAuthenticated = require("../middleware/isAuthenticated");
const isAdmin = require("../middleware/isAdmin");
const isAuthorized = require("../middleware/isAuthorized");

// Public routes
router.post("/login", login);
router.post("/register", register);
router.post("/activate", activateAccount);
router.post("/forgot-password", forgotPassword);
router.post("/reset-password", resetPassword);

// Admin routes
router.get("/users", isAuthenticated, isAdmin, getUsers);
router.get("/users/:id", isAuthenticated, isAdmin, getUserById);
router.post("/users", isAuthenticated, isAdmin, createUser);
router.put("/users", isAuthenticated, isAuthorized("updateUser"), updateUser);
router.post(
  "/users/set-current-organization",
  isAuthenticated,
  setCurrentOrganizationForUser
);
router.delete(
  "/users",
  isAuthenticated,
  isAuthorized("deleteUser"),
  deleteUser
);

// Token management routes
router.post("/refresh-token", refreshToken);
router.post("/logout", isAuthenticated, logout);

module.exports = router;
