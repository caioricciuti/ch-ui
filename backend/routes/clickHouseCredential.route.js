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
  getAllClickHouseCredentials,
} = require("../controllers/clickHouseCredential.controller");

const isAuthenticated = require("../middleware/isAuthenticated");
const isAdmin = require("../middleware/isAdmin");
const isAuthorized = require("../middleware/isAuthorized");

// Apply authentication middleware to all routes
router.use(isAuthenticated);

// CRUD operations
router.post("/", isAdmin, createClickHouseCredential);
router.get(
  "/:id",
  isAuthorized("viewClickHouseCredential"),
  getClickHouseCredentialById
);
router.get("/", isAuthenticated, getAllClickHouseCredentials);
router.put(
  "/:id",
  isAuthorized("updateClickHouseCredential"),
  updateClickHouseCredential
);
router.delete("/:id", isAdmin, deleteClickHouseCredential);

// Organization assignment operations
router.post("/:id/organizations", isAdmin, assignCredentialToOrganization);
router.delete(
  "/:id/organizations/:organizationId",
  isAdmin,
  revokeCredentialFromOrganization
);

// User assignment operations
router.post("/:id/users", isAdmin, assignUserToCredential);
router.delete("/:id/users/:userId", isAdmin, revokeUserFromCredential);

module.exports = router;
