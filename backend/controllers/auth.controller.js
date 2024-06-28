const bcrypt = require("bcrypt");
const User = require("../models/User");
const crypto = require("crypto");
const { body, validationResult } = require("express-validator");
const jwt = require("jsonwebtoken");
const errorResponse = require("../utils/errorResponse");
const isAdmin = require("../middleware/isAdmin");
require("dotenv").config();

// Helper function to generate JWT tokens
const generateTokens = (user) => {
  const accessToken = jwt.sign(
    { id: user._id, role: user.role },
    process.env.ACCESS_TOKEN_SECRET,
    { expiresIn: process.env.ACCESS_TOKEN_EXPIRES_IN }
  );

  const refreshToken = jwt.sign(
    { id: user._id, role: user.role },
    process.env.REFRESH_TOKEN_SECRET,
    { expiresIn: process.env.REFRESH_TOKEN_EXPIRES_IN }
  );

  return { accessToken, refreshToken };
};

exports.login = [
  body("email").isEmail().trim().notEmpty(),
  body("password").isString().notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "login",
        errors
      );
    }

    try {
      const { email, password } = req.body;
      const user = await User.findOne({ email }).select("+password");

      if (!user) {
        return errorResponse(res, 400, 1009, "User does not exist", "login");
      }

      const isMatch = await user.matchPassword(password);
      if (!isMatch) {
        return errorResponse(res, 400, 1010, "Incorrect password", "login");
      }

      const { accessToken, refreshToken } = generateTokens(user);

      res.cookie("accessToken", accessToken, {
        httpOnly: true,
        secure: process.env.NODE_ENV === "production",
        maxAge: 15 * 60 * 1000, // 15 minutes USING SERVER TIME
      });

      res.cookie("refreshToken", refreshToken, {
        httpOnly: true,
        secure: process.env.NODE_ENV === "production",
        maxAge: 7 * 24 * 60 * 60 * 1000,
      });

      res.json({ message: "Login successful" });
    } catch (error) {
      errorResponse(res, 500, 1011, "Failed to login", "login", error);
    }
  },
];

exports.register = [
  body("name").isString().isLength({ max: 32 }).trim().notEmpty(),
  body("email").isEmail().trim().notEmpty(),
  body("password")
    .isString()
    .isLength({ min: 8 })
    .matches(/[A-Z]/)
    .withMessage("Password must contain at least one uppercase letter")
    .matches(/\W/)
    .withMessage("Password must contain at least one special character")
    .notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "register",
        errors
      );
    }

    try {
      const { name, email, password } = req.body;
      const user = await User.findOne({ email });

      if (user) {
        return errorResponse(res, 400, 1012, "User already exists", "register");
      }

      const newUser = new User({ name, email, password });
      const activationToken = crypto.randomBytes(20).toString("hex");
      newUser.activationToken = activationToken;
      newUser.activationTokenExpire = Date.now() + 3600000; // 1 hour

      await newUser.save();

      const { accessToken, refreshToken } = generateTokens(newUser);

      res.cookie("accessToken", accessToken, {
        httpOnly: true,
        secure: process.env.NODE_ENV === "production",
        maxAge: 15 * 60 * 1000,
      });

      res.cookie("refreshToken", refreshToken, {
        httpOnly: true,
        secure: process.env.NODE_ENV === "production",
        maxAge: 7 * 24 * 60 * 60 * 1000,
      });

      res.status(201).json({ message: "User registered successfully" });
    } catch (error) {
      errorResponse(
        res,
        500,
        1013,
        "Failed to register user",
        "register",
        error
      );
    }
  },
];

exports.forgotPassword = [
  body("email").isEmail().trim().notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "forgotPassword",
        errors
      );
    }

    try {
      const { email } = req.body;
      const user = await User.findOne({ email });

      if (!user) {
        return errorResponse(
          res,
          400,
          1009,
          "User does not exist",
          "forgotPassword"
        );
      }

      const resetToken = crypto.randomBytes(20).toString("hex");
      user.resetPasswordToken = resetToken;
      user.resetPasswordExpire = Date.now() + 3600000; // 1 hour

      await user.save();
      res.json({ message: "Reset password email sent", resetToken });
    } catch (error) {
      errorResponse(
        res,
        500,
        1014,
        "Failed to send reset password email",
        "forgotPassword",
        error
      );
    }
  },
];

exports.resetPassword = [
  body("resetToken").isString().notEmpty(),
  body("password")
    .isString()
    .isLength({ min: 8 })
    .matches(/[A-Z]/)
    .withMessage("Password must contain at least one uppercase letter")
    .matches(/\W/)
    .withMessage("Password must contain at least one special character")
    .notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "resetPassword",
        errors
      );
    }

    try {
      const { resetToken, password } = req.body;
      const user = await User.findOne({
        resetPasswordToken: resetToken,
        resetPasswordExpire: { $gt: Date.now() },
      });

      if (!user) {
        return errorResponse(
          res,
          400,
          1009,
          "Invalid or expired token",
          "resetPassword"
        );
      }

      user.password = password;
      user.resetPasswordToken = undefined;
      user.resetPasswordExpire = undefined;

      await user.save();
      res.json({ message: "Password reset successfully" });
    } catch (error) {
      errorResponse(
        res,
        500,
        1015,
        "Failed to reset password",
        "resetPassword",
        error
      );
    }
  },
];

exports.activateAccount = [
  body("activationToken").isString().notEmpty(),
  body("email").isEmail().trim().notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "activateAccount",
        errors
      );
    }

    try {
      const { activationToken, email } = req.body;
      const user = await User.findOne({
        email,
        activationToken,
        // activationTokenExpire: { $gt: Date.now() },
      });

      if (!user) {
        return errorResponse(
          res,
          400,
          1009,
          "Invalid or expired token",
          "activateAccount"
        );
      }

      if (user.activationTokenExpire < Date.now()) {
        // delete the token and expire time
        user.activationToken = null;
        user.activationTokenExpire = null;
        await user.save();
        return errorResponse(
          res,
          400,
          1009,
          "Invalid or expired token - Any tokens you had are now deleted for your safety. Please request a new one.",
          "activateAccount"
        );
      }

      user.active = true;
      user.activationToken = undefined;
      user.activationTokenExpire = undefined;

      await user.save();
      res.json({ message: "Account activated successfully" });
    } catch (error) {
      errorResponse(
        res,
        500,
        1016,
        "Failed to activate account",
        "activateAccount",
        error
      );
    }
  },
];

exports.refreshToken = async (req, res) => {
  const refreshToken = req.cookies.refreshToken;
  if (!refreshToken) {
    return errorResponse(
      res,
      401,
      1005,
      "No refresh token provided",
      "refreshToken"
    );
  }

  try {
    const decoded = jwt.verify(refreshToken, process.env.REFRESH_TOKEN_SECRET);
    const user = await User.findById(decoded.id);
    if (!user) {
      return errorResponse(
        res,
        401,
        1017,
        "Invalid refresh token",
        "refreshToken"
      );
    }

    const { accessToken, refreshToken: newRefreshToken } = generateTokens(user);

    res.cookie("accessToken", accessToken, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      maxAge: 15 * 60 * 1000,
    });

    res.cookie("refreshToken", newRefreshToken, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      maxAge: 7 * 24 * 60 * 60 * 1000,
    });

    res.json({ message: "Token refreshed successfully" });
  } catch (error) {
    errorResponse(
      res,
      401,
      1018,
      "Failed to refresh token",
      "refreshToken",
      error
    );
  }
};

exports.logout = async (req, res) => {
  res.cookie("accessToken", "", { maxAge: 0 });
  res.cookie("refreshToken", "", { maxAge: 0 });
  res.json({ message: "Logged out successfully" });
};

// TO-DO Implement:

/* 
1. Add controller to re-generate activation token (if user did not receive the email, or the token expired)
*/

exports.resetActivationToken = [
  body("email").isEmail().trim().notEmpty(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "resetActivationToken",
        errors
      );
    }

    try {
      const { email } = req.body;
      const user = await User.findOne({ email });

      if (!user) {
        return errorResponse(
          res,
          400,
          1009,
          "User does not exist",
          "resetActivationToken"
        );
      }

      //if user is already active, return error
      if (user.active) {
        return errorResponse(
          res,
          400,
          1019,
          "User is already active",
          "resetActivationToken"
        );
      }

      const activationToken = crypto.randomBytes(20).toString("hex");
      user.activationToken = activationToken;
      user.activationTokenExpire = Date.now() + 3600000; // 1 hour

      await user.save();
      res.json({ message: "Activation token reset successfully" });
    } catch (error) {
      errorResponse(
        res,
        500,
        1019,
        "Failed to reset activation token",
        "resetActivationToken",
        error
      );
    }
  },
];

module.exports = exports;
