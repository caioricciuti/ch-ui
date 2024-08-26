const User = require("../models/User");
const crypto = require("crypto");
const { body, validationResult } = require("express-validator");
const jwt = require("jsonwebtoken");
const errorResponse = require("../utils/errorResponse");
const { promisify } = require("util");
require("dotenv").config();

// Helper function to generate JWT tokens
const generateTokens = (user) => {
  try {
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
  } catch (error) {
    console.error("Token generation error:", error);
    throw new Error("Failed to generate tokens");
  }
};

// Helper function for setting cookies
const setCookies = (res, { accessToken, refreshToken }) => {
  const secureCookie = process.env.NODE_ENV === "production";
  res.cookie("accessToken", accessToken, {
    httpOnly: true,
    secure: secureCookie,
    sameSite: "strict",
    maxAge: 15 * 60 * 1000, // 15 minutes
  });

  res.cookie("refreshToken", refreshToken, {
    httpOnly: true,
    secure: secureCookie,
    sameSite: "strict",
    maxAge: 7 * 24 * 60 * 60 * 1000, // 7 days
  });
};

// Helper function for password validation
const passwordValidation = body("password")
  .isString()
  .isLength({ min: 8 })
  .matches(/^(?=.*[A-Z])(?=.*\d)(?=.*[!@#$%^&*(),.?":{}|<>])/)
  .withMessage(
    "Password must be at least 8 characters long, contain at least one uppercase letter, one number, and one special character"
  );

exports.login = [
  body("email").isEmail().normalizeEmail().trim(),
  body("password").isString(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "login",
        errors.array()
      );
    }

    try {
      const { email, password } = req.body;
      const user = await User.findOne({ email }).select("+password");

      if (!user || !(await user.matchPassword(password))) {
        return errorResponse(res, 401, 1010, "Invalid credentials", "login");
      }

      if (!user.active) {
        return errorResponse(res, 403, 1020, "Account not activated", "login");
      }

      const tokens = generateTokens(user);
      setCookies(res, tokens);

      res.json({
        message: "Login successful",
        user: {
          id: user._id,
          name: user.name,
          email: user.email,
          role: user.role,
        },
      });
    } catch (error) {
      console.error("Login error:", error);
      errorResponse(res, 500, 1011, "Failed to login", "login");
    }
  },
];

exports.register = [
  body("name").isString().trim().isLength({ min: 2, max: 50 }),
  body("email").isEmail().normalizeEmail().trim(),
  passwordValidation,

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "register",
        errors.array()
      );
    }

    try {
      const { name, email, password } = req.body;

      const existingUser = await User.findOne({ email });
      if (existingUser) {
        return errorResponse(res, 409, 1012, "User already exists", "register");
      }

      const newUser = new User({
        name,
        email,
        password,
        activationToken: crypto.randomBytes(32).toString("hex"),
        activationTokenExpire: Date.now() + 24 * 60 * 60 * 1000, // 24 hours
      });

      await newUser.save();

      // TODO: Send activation email

      res
        .status(201)
        .json({
          message:
            "User registered successfully. Please check your email to activate your account.",
        });
    } catch (error) {
      console.error("Registration error:", error);
      errorResponse(res, 500, 1013, "Failed to register user", "register");
    }
  },
];

exports.forgotPassword = [
  body("email").isEmail().normalizeEmail().trim(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "forgotPassword",
        errors.array()
      );
    }

    try {
      const { email } = req.body;
      const user = await User.findOne({ email });

      if (!user) {
        // Don't reveal user existence, return a generic message
        return res.json({
          message:
            "If a user with that email exists, a password reset link has been sent.",
        });
      }

      user.resetPasswordToken = crypto.randomBytes(32).toString("hex");
      user.resetPasswordExpire = Date.now() + 60 * 60 * 1000; // 1 hour

      await user.save();

      // TODO: Send password reset email

      res.json({
        message:
          "If a user with that email exists, a password reset link has been sent.",
      });
    } catch (error) {
      console.error("Forgot password error:", error);
      errorResponse(
        res,
        500,
        1014,
        "Failed to process forgot password request",
        "forgotPassword"
      );
    }
  },
];

exports.resetPassword = [
  body("resetToken").isString().trim(),
  passwordValidation,

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "resetPassword",
        errors.array()
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
      console.error("Reset password error:", error);
      errorResponse(
        res,
        500,
        1015,
        "Failed to reset password",
        "resetPassword"
      );
    }
  },
];

exports.activateAccount = [
  body("activationToken").isString().trim(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "activateAccount",
        errors.array()
      );
    }

    try {
      const { activationToken } = req.body;
      const user = await User.findOne({
        activationToken,
        activationTokenExpire: { $gt: Date.now() },
      });

      if (!user) {
        return errorResponse(
          res,
          400,
          1009,
          "Invalid or expired activation token",
          "activateAccount"
        );
      }

      user.active = true;
      user.activationToken = undefined;
      user.activationTokenExpire = undefined;

      await user.save();
      res.json({ message: "Account activated successfully" });
    } catch (error) {
      console.error("Account activation error:", error);
      errorResponse(
        res,
        500,
        1016,
        "Failed to activate account",
        "activateAccount"
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
    const decoded = await promisify(jwt.verify)(
      refreshToken,
      process.env.REFRESH_TOKEN_SECRET
    );
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

    const tokens = generateTokens(user);
    setCookies(res, tokens);

    res.json({ message: "Token refreshed successfully" });
  } catch (error) {
    console.error("Refresh token error:", error);
    errorResponse(res, 401, 1018, "Failed to refresh token", "refreshToken");
  }
};

exports.logout = async (req, res) => {
  res.cookie("accessToken", "", {
    maxAge: 0,
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
  });
  res.cookie("refreshToken", "", {
    maxAge: 0,
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
  });
  res.json({ message: "Logged out successfully" });
};

exports.resetActivationToken = [
  body("email").isEmail().normalizeEmail().trim(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "resetActivationToken",
        errors.array()
      );
    }

    try {
      const { email } = req.body;
      const user = await User.findOne({ email, active: false });

      if (!user) {
        // Don't reveal user existence, return a generic message
        return res.json({
          message:
            "If a non-activated user with that email exists, a new activation link has been sent.",
        });
      }

      user.activationToken = crypto.randomBytes(32).toString("hex");
      user.activationTokenExpire = Date.now() + 24 * 60 * 60 * 1000; // 24 hours

      await user.save();

      // TODO: Send new activation email

      res.json({
        message:
          "If a non-activated user with that email exists, a new activation link has been sent.",
      });
    } catch (error) {
      console.error("Reset activation token error:", error);
      errorResponse(
        res,
        500,
        1019,
        "Failed to reset activation token",
        "resetActivationToken"
      );
    }
  },
];

module.exports = exports;
