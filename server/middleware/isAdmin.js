const User = require("../models/User");
const errorResponse = require("../utils/errorResponse");

const isAdmin = async (req, res, next) => {
  try {
    // Check if req.user exists (should be set by isAuthenticated middleware)
    if (!req.user) {
      return errorResponse(res, 401, 3001, "User not authenticated", "isAdmin");
    }

    // Check if the user's role is already in req.user
    if (req.user.role === "admin") {
      return next();
    }

    // If role is not in req.user, fetch it from the database
    const user = await User.findById(req.user.id).select("role");

    if (!user) {
      return errorResponse(res, 404, 3002, "User not found", "isAdmin");
    }

    if (user.role !== "admin") {
      return errorResponse(
        res,
        403,
        3003,
        "Access denied. Admin privileges required",
        "isAdmin"
      );
    }

    // Update req.user with the fetched role for future use
    req.user.role = user.role;

    next();
  } catch (error) {
    console.error("isAdmin middleware error:", error);
    errorResponse(res, 500, 3004, "Internal server error", "isAdmin");
  }
};

module.exports = isAdmin;
