// socketAuth.js
const jwt = require("jsonwebtoken");
const { promisify } = require("util");
const User = require("../models/User");

const verifyJWT = promisify(jwt.verify);

const socketAuth = async (socket, next) => {
  const cookies = socket.request.headers.cookie
    ?.split("; ")
    .reduce((acc, cookie) => {
      const [key, value] = cookie.split("=");
      acc[key] = value;
      return acc;
    }, {});

  const accessToken = cookies?.accessToken;
  const refreshToken = cookies?.refreshToken;

  if (!accessToken) {
    return next(new Error("Access token missing"));
  }

  try {
    const decoded = await verifyJWT(
      accessToken,
      process.env.ACCESS_TOKEN_SECRET
    );
    const user = await User.findById(decoded.id)
      .select("+active")
      .populate("activeOrganization")
      .populate("activeClickhouseCredential");

    if (!user || !user.active) {
      return next(new Error("User not found or inactive"));
    }

    socket.user = user;
    next();
  } catch (err) {
    if (err.name === "TokenExpiredError" && refreshToken) {
      try {
        const refreshDecoded = await verifyJWT(
          refreshToken,
          process.env.REFRESH_TOKEN_SECRET
        );
        const user = await User.findById(refreshDecoded.id).select("+active");

        if (!user || !user.active) {
          return next(new Error("User not found or inactive"));
        }

        const newAccessToken = jwt.sign(
          { id: user._id, role: user.role },
          process.env.ACCESS_TOKEN_SECRET,
          { expiresIn: process.env.ACCESS_TOKEN_EXPIRES_IN }
        );

        socket.user = user;
        socket.emit("new_access_token", newAccessToken);
        next();
      } catch (refreshErr) {
        return next(new Error("Invalid refresh token"));
      }
    } else {
      return next(new Error("Invalid access token"));
    }
  }
};

module.exports = socketAuth;
