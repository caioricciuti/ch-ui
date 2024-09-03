const jwt = require("jsonwebtoken");
const { promisify } = require("util");
const User = require("../models/User");
const errorResponse = require("../utils/errorResponse");
require("dotenv").config();

const verifyJWT = promisify(jwt.verify);

class AuthError extends Error {
  constructor(statusCode, errorCode, message) {
    super(message);
    this.statusCode = statusCode;
    this.errorCode = errorCode;
  }
}

const generateNewAccessToken = (user) => {
  return jwt.sign(
    { id: user._id },
    process.env.ACCESS_TOKEN_SECRET,
    { expiresIn: process.env.ACCESS_TOKEN_EXPIRES_IN }
  );
};

const verifyToken = async (token, secret) => {
  try {
    return await verifyJWT(token, secret);
  } catch (error) {
    if (error.name === "TokenExpiredError") {
      throw new AuthError(401, 3009, "Token expired");
    }
    throw new AuthError(401, 3007, "Invalid token");
  }
};

const findAndValidateUser = async (userId) => {
  const user = await User.findById(userId)
    .select("+active")
    .populate("activeOrganization")
    .populate("activeClickhouseCredential");

  if (!user || !user.active) {
    throw new AuthError(401, 3008, "User not found or inactive");
  }

  return user;
};

const isAuthenticated = async (req, res, next) => {
  const accessToken = req.cookies.accessToken;
  const refreshToken = req.cookies.refreshToken;

  if (!accessToken) {
    return errorResponse(res, 401, 3004, "Access token missing", "isAuthenticated");
  }

  try {
    const decoded = await verifyToken(accessToken, process.env.ACCESS_TOKEN_SECRET);
    req.user = await findAndValidateUser(decoded.id);
    return next();
  } catch (error) {
    if (error.errorCode === 3009 && refreshToken) { // Token expired
      try {
        const refreshDecoded = await verifyToken(refreshToken, process.env.REFRESH_TOKEN_SECRET);
        const user = await findAndValidateUser(refreshDecoded.id);

        const newAccessToken = generateNewAccessToken(user);

        res.cookie("accessToken", newAccessToken, {
          httpOnly: true,
          secure: process.env.NODE_ENV === "production",
          sameSite: "strict",
          maxAge: 15 * 60 * 1000, // 15 minutes
        });

        req.user = user;
        return next();
      } catch (refreshError) {
        return errorResponse(res, 401, 3006, "Invalid refresh token", "isAuthenticated");
      }
    } else {
      return errorResponse(res, error.statusCode, error.errorCode, error.message, "isAuthenticated");
    }
  }
};

module.exports = isAuthenticated;