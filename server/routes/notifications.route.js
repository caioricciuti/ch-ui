const express = require("express");
const router = express.Router();
const notificationController = require("../controllers/notifications.controller");
const isAuthenticated = require("../middleware/isAuthenticated");

router.get("/", isAuthenticated, notificationController.getNotifications);
router.put("/read", isAuthenticated, notificationController.markAsRead);
router.put("/read-all", isAuthenticated, notificationController.markAllAsRead);
router.delete(
  "/delete",
  isAuthenticated,
  notificationController.deleteNotification
);
router.delete(
  "/delete-all",
  isAuthenticated,
  notificationController.deleteAllNotifications
);

module.exports = router;
