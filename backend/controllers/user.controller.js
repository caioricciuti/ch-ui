const bcrypt = require("bcrypt");
const User = require("../models/User");
const crypto = require("crypto");
const { body, validationResult } = require("express-validator");
const jwt = require("jsonwebtoken");
const mongoose = require("mongoose");
const errorResponse = require("../utils/errorResponse");
require("dotenv").config();

// Helper function to generate JWT tokens
const generateTokens = (user) => {
  const accessToken = jwt.sign(
    { id: user._id, role: user.role },
    process.env.ACCESS_TOKEN_SECRET,
    { expiresIn: "15m" }
  );

  const refreshToken = jwt.sign(
    { id: user._id, role: user.role },
    process.env.REFRESH_TOKEN_SECRET,
    { expiresIn: "7d" }
  );

  return { accessToken, refreshToken };
};

// get all users
exports.getUsers = async (req, res) => {
  try {
    const users = await User.find({});
    res.json(users);
  } catch (error) {
    errorResponse(res, 500, 1001, "Failed to fetch users", "getUsers", error);
  }
};

// get user by id
exports.getUserById = async (req, res) => {
  try {
    if (!mongoose.Types.ObjectId.isValid(req.params.id)) {
      return errorResponse(res, 400, 1004, "Invalid user ID", "getUserById");
    }

    const user = await User.findById(req.params.id);
    if (!user) {
      return errorResponse(res, 404, 1002, "User not found", "getUserById");
    }
    res.json(user);
  } catch (error) {
    errorResponse(res, 500, 1003, "Failed to fetch user", "getUserById", error);
  }
};

// create user
exports.createUser = [
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
  body("role").optional().isIn(["admin", "user"]),
  body("organization").optional().isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "createUser",
        errors
      );
    }

    try {
      const user = new User(req.body);
      await user.save();
      res.status(201).json(user);
    } catch (error) {
      errorResponse(
        res,
        500,
        1006,
        "Failed to create user",
        "createUser",
        error
      );
    }
  },
];

// update user
exports.updateUser = [
  body("userId").isMongoId(),
  body("name").optional().isString().isLength({ max: 32 }).trim(),
  body("email").optional().isEmail().trim(),
  body("password")
    .optional()
    .isString()
    .isLength({ min: 8 })
    .matches(/[A-Z]/)
    .withMessage("Password must contain at least one uppercase letter")
    .matches(/\W/)
    .withMessage("Password must contain at least one special character"),
  body("role").optional().isIn(["admin", "user"]),
  body("organization").optional().isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "updateUser",
        errors
      );
    }

    try {
      const user = await User.findById(req.body.userId);
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "updateUser");
      }

      user.set(req.body);
      await user.save();
      res.json(user);
    } catch (error) {
      errorResponse(
        res,
        500,
        1007,
        "Failed to update user",
        "updateUser",
        error
      );
    }
  },
];

// delete user
exports.deleteUser = [
  body("userId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "deleteUser",
        errors
      );
    }

    try {
      const user = await User.findByIdAndDelete(req.body.userId);
      if (!user) {
        return errorResponse(res, 404, 1002, "User not found", "deleteUser");
      }
      res.json({ message: "User deleted successfully", user });
    } catch (error) {
      errorResponse(
        res,
        500,
        1008,
        "Failed to delete user",
        "deleteUser",
        error
      );
    }
  },
];

// user login
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
      const user = await User.findOne({ email });

      if (!user) {
        return errorResponse(res, 400, 1009, "User does not exist", "login");
      }

      const isMatch = await bcrypt.compare(password, user.password);
      if (!isMatch) {
        return errorResponse(res, 400, 1010, "Incorrect password", "login");
      }

      const { accessToken, refreshToken } = generateTokens(user);

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

      res.json({ message: "Login successful" });
    } catch (error) {
      errorResponse(res, 500, 1011, "Failed to login", "login", error);
    }
  },
];

// user register
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
      const salt = await bcrypt.genSalt(10);
      newUser.password = await bcrypt.hash(password, salt);

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

// user forgot password
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

// user reset password
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

      const salt = await bcrypt.genSalt(10);
      user.password = await bcrypt.hash(password, salt);
      user.resetPasswordToken = null;
      user.resetPasswordExpire = null;

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

// user activate account
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
        activationTokenExpire: { $gt: Date.now() },
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

      user.active = true;
      user.activationToken = null;
      user.activationTokenExpire = null;

      await user.save();
      res.json({ message: "Account activated successfully", user });
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

// refresh token
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

// user logout
exports.logout = async (req, res) => {
  res.cookie("accessToken", "", { maxAge: 0 });
  res.cookie("refreshToken", "", { maxAge: 0 });
  res.json({ message: "Logged out successfully" });
};

// user set current organization

exports.setCurrentOrganizationForUser = [
  body("userId").isMongoId(),
  body("organizationId").isMongoId(),

  async (req, res) => {
    const errors = validationResult(req);
    if (!errors.isEmpty()) {
      return errorResponse(
        res,
        400,
        1005,
        "Validation errors",
        "setCurrentOrganizationForUser",
        errors
      );
    }

    try {
      const user = await User.findById(req.body.userId);
      if (!user) {
        return errorResponse(
          res,
          404,
          1002,
          "User not found",
          "setCurrentOrganizationForUser"
        );
      }

      user.activeOrganization = req.body.organizationId;
      await user.save();
      res.json({ message: "Current organization set successfully" });
    } catch (error) {
      errorResponse(
        res,
        500,
        1019,
        "Failed to set current organization",
        "setCurrentOrganizationForUser",
        error
      );
    }
  },
];
