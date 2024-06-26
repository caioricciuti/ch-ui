const User = require("../models/User");
const errorResponse = require("../utils/errorResponse");

const isAdmin = async (req, res, next) => {
  try {
    const user = await User.findById(req.user.id);
    if (!user || user.role !== "admin") {
      return errorResponse(
        res,
        403,
        3002,
        "Access denied, admin only",
        "isAdmin"
      );
    }
    next();
  } catch (error) {
    errorResponse(res, 500, 3003, "Internal server error", "isAdmin", error);
  }
};

module.exports = isAdmin;
