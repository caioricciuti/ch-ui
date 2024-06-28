const express = require("express");
const router = express.Router();

const {
  createClickHouseCredential,
  getClickHouseCredentialById,
  updateClickHouseCredential,
  deleteClickHouseCredential,
  assignCredentialToOrganization,
  revokeCredentialFromOrganization,
  assignUserToCredential,
  revokeUserFromCredential,
} = require("../controllers/clickHouseCredential.controller");

// Middleware for checking JWT and Authorization
const isAuthenticated = require("../middleware/isAuthenticated");
const isAdmin = require("../middleware/isAdmin");

// Routes
router.post("/", isAuthenticated, createClickHouseCredential);

router.get("/:id", isAuthenticated, getClickHouseCredentialById);
router.put("/:id", isAuthenticated, updateClickHouseCredential);
router.delete("/:id", isAuthenticated, deleteClickHouseCredential);
router.post(
  "/assign-organization",
  isAuthenticated,
  isAdmin,
  assignCredentialToOrganization
);
router.post(
  "/revoke-organization",
  isAuthenticated,
  isAdmin,
  revokeCredentialFromOrganization
);
router.post("/assign-user", isAuthenticated, isAdmin, assignUserToCredential);
router.post("/revoke-user", isAuthenticated, isAdmin, revokeUserFromCredential);

module.exports = router;
