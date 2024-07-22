const jwt = require("jsonwebtoken");
const { promisify } = require("util");
const User = require("../models/User");
const errorResponse = require("../utils/errorResponse");
require("dotenv").config();

const verifyJWT = promisify(jwt.verify);

const generateNewAccessToken = (user) => {
  return jwt.sign(
    { id: user._id, role: user.role },
    process.env.ACCESS_TOKEN_SECRET,
    { expiresIn: process.env.ACCESS_TOKEN_EXPIRES_IN }
  );
};

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
    const decoded = await verifyJWT(
      accessToken,
      process.env.ACCESS_TOKEN_SECRET
    );
    const user = await User.findById(decoded.id)
      .select("+active")
      .populate("activeOrganization");

    if (!user || !user.active) {
      return errorResponse(
        res,
        401,
        3008,
        "User not found or inactive",
        "isAuthenticated"
      );
    }

    req.user = user;
    return next();
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
        const refreshDecoded = await verifyJWT(
          refreshToken,
          process.env.REFRESH_TOKEN_SECRET
        );
        const user = await User.findById(refreshDecoded.id).select("+active");

        if (!user || !user.active) {
          return errorResponse(
            res,
            401,
            3008,
            "User not found or inactive",
            "isAuthenticated"
          );
        }

        const newAccessToken = generateNewAccessToken(user);

        res.cookie("accessToken", newAccessToken, {
          httpOnly: true,
          secure: process.env.NODE_ENV === "production",
          sameSite: "strict",
          maxAge: 15 * 60 * 1000, // 15 minutes
        });

        req.user = user;
        return next();
      } catch (refreshErr) {
        return errorResponse(
          res,
          401,
          3006,
          "Invalid refresh token",
          "isAuthenticated"
        );
      }
    } else {
      return errorResponse(
        res,
        401,
        3007,
        "Invalid access token",
        "isAuthenticated"
      );
    }
  }
};

module.exports = isAuthenticated;
