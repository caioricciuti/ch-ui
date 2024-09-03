const Notification = require("../models/Notifications");
const User = require("../models/User");
const { body, validationResult } = require("express-validator");
const errorResponse = require("../utils/errorResponse");

// Get Notifications (Latest 20)
exports.getNotifications = async (req, res) => {
  try {
    const notifications = await Notification.find({ userId: req.user.id })
      .sort({ createdAt: -1 })
      .limit(20);
    res.json({ data: notifications });
  } catch (error) {
    console.error("Get notifications error:", error);
    errorResponse(
      res,
      500,
      2001,
      "Failed to fetch notifications",
      "getNotifications"
    );
  }
};

// Get Unread Notification Count
exports.getUnreadCount = async (req, res) => {
  try {
    const count = await Notification.countDocuments({
      userId: req.user.id,
      read: false,
    });
    res.json({ count });
  } catch (error) {
    console.error("Get unread count error:", error);
    errorResponse(
      res,
      500,
      2002,
      "Failed to fetch unread count",
      "getUnreadCount"
    );
  }
};

// Mark Notification as Read
exports.markAsRead = [
  body("id").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        2003,
        "Validation errors",
        "markAsRead",
        errors.array()
      );
    }

    try {
      const notification = await Notification.findOneAndUpdate(
        { _id: req.body.id, userId: req.user.id },
        { read: true },
        { new: true }
      );
      if (!notification) {
        return errorResponse(
          res,
          404,
          2004,
          "Notification not found",
          "markAsRead"
        );
      }
      res.json(notification);
    } catch (error) {
      console.error("Mark as read error:", error);
      errorResponse(
        res,
        500,
        2005,
        "Failed to mark notification as read",
        "markAsRead"
      );
    }
  },
];

// Mark All Notifications as Read
exports.markAllAsRead = async (req, res) => {
  try {
    await Notification.updateMany(
      { userId: req.user.id, read: false },
      { read: true }
    );
    res.json({ message: "All notifications marked as read" });
  } catch (error) {
    console.error("Mark all as read error:", error);
    errorResponse(
      res,
      500,
      2006,
      "Failed to mark all notifications as read",
      "markAllAsRead"
    );
  }
};

// Delete a Notification
exports.deleteNotification = async (req, res) => {
  try {
    const notification = await Notification.findOneAndDelete({
      _id: req.body.id,
      userId: req.user.id,
    });
    if (!notification) {
      return errorResponse(
        res,
        404,
        2007,
        "Notification not found",
        "deleteNotification"
      );
    }
    res.json({ message: "Notification deleted", notification });
  } catch (error) {
    console.error("Delete notification error:", error);
    errorResponse(
      res,
      500,
      2008,
      "Failed to delete notification",
      "deleteNotification"
    );
  }
};

// Delete All Notifications
exports.deleteAllNotifications = async (req, res) => {
  try {
    await Notification.deleteMany({ userId: req.user.id });
    res.json({ message: "All notifications deleted" });
  } catch (error) {
    console.error("Delete all notifications error:", error);
    errorResponse(
      res,
      500,
      2009,
      "Failed to delete all notifications",
      "deleteAllNotifications"
    );
  }
};

// Create a Notification (internal usage)
exports.createNotification = async (userId, title, message, type = "info") => {
  try {
    const user = await User.findById(userId);
    if (!user) {
      throw new Error("User not found");
    }

    const notification = new Notification({
      userId,
      title,
      message,
      type,
    });
    await notification.save();
    return notification;
  } catch (error) {
    console.error("Create notification error:", error);
    throw error;
  }
};
