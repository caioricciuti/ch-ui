const express = require("express");
const router = express.Router();

const {
  login,
  register,
  forgotPassword,
  resetPassword,
  activateAccount,
  refreshToken,
  resetActivationToken,
  logout,
} = require("../controllers/auth.controller");

router.post("/login", login);
router.post("/register", register);
router.post("/forgot-password", forgotPassword);
router.post("/reset-password", resetPassword);
router.post("/activate-account", activateAccount);
router.post("/refresh-token", refreshToken);
router.post("/reset-activation-token", resetActivationToken);
router.post("/logout", logout);

module.exports = router;
