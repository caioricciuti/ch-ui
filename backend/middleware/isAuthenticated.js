const jwt = require("jsonwebtoken");
const User = require("../models/User");
const errorResponse = require("../utils/errorResponse");
require("dotenv").config();

const isAuthenticated = async (req, res, next) => {
  const accessToken = req.cookies.accessToken;
  const refreshToken = req.cookies.refreshToken;

  if (!accessToken) {
    return errorResponse(
      res,
      401,
      3004,
      "Access token missing",
      "isAuthenticated"
    );
  }

  try {
    const decoded = jwt.verify(accessToken, process.env.ACCESS_TOKEN_SECRET);
    const user = await User.findById(decoded.id).populate("activeOrganization");

    if (!user) {
      return errorResponse(res, 401, 3008, "User not found", "isAuthenticated");
    }

    req.user = user;
    next();
  } catch (err) {
    if (err.name === "TokenExpiredError") {
      if (!refreshToken) {
        return errorResponse(
          res,
          401,
          3005,
          "Refresh token missing",
          "isAuthenticated"
        );
      }

      try {
        const refreshDecoded = jwt.verify(
          refreshToken,
          process.env.REFRESH_TOKEN_SECRET
        );
        const user = await User.findById(refreshDecoded.id);

        if (!user) {
          return errorResponse(
            res,
            401,
            3008,
            "User not found",
            "isAuthenticated"
          );
        }

        const newAccessToken = jwt.sign(
          { id: user._id, role: user.role },
          process.env.ACCESS_TOKEN_SECRET,
          { expiresIn: process.env.ACCESS_TOKEN_EXPIRES_IN }
        );

        res.cookie("accessToken", newAccessToken, {
          httpOnly: true,
          secure: process.env.NODE_ENV === "production",
          maxAge: 15 * 60 * 1000, // 15 minutes
        });

        req.user = user;
        next();
      } catch (refreshErr) {
        return errorResponse(
          res,
          401,
          3006,
          "Invalid refresh token",
          "isAuthenticated",
          refreshErr
        );
      }
    } else {
      return errorResponse(
        res,
        401,
        3007,
        "Invalid access token",
        "isAuthenticated",
        err
      );
    }
  }
};

module.exports = isAuthenticated;
