const User = require("../models/User");
// Error Response // code 300x - Middleware
const errorResponse = require("../utils/errorResponse");

const isAuthorized = (action) => {
  return async (req, res, next) => {
    try {
      const user = await User.findById(req.user.id);
      if (!user) {
        return errorResponse(res, 401, 3001, "User not found", "isAuthorized");
      }

      // Check if the user is an admin
      if (user.role === "admin") {
        return next();
      }

      // Define permissions
      const permissions = {
        updateUser: user._id.toString() === req.body.userId,
        deleteUser: user._id.toString() === req.body.userId,
      };

      // Check if the user is authorized to perform the action
      if (permissions[action]) {
        return next();
      }

      return errorResponse(res, 403, 3002, "Access denied", "isAuthorized");
    } catch (error) {
      errorResponse(
        res,
        500,
        3003,
        "Internal server error",
        "isAuthorized",
        error
      );
    }
  };
};

module.exports = isAuthorized;
