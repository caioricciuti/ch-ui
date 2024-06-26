const ClickHouseCredential = require("../models/ClickHouseCredential");
const errorResponse = require("../utils/errorResponse");

// Create ClickHouse credential
exports.createClickHouseCredential = async (req, res) => {
  const { users, name, host, username, password, allowedOrganizations } =
    req.body;

  try {
    const credential = new ClickHouseCredential({
      users: Array.isArray(users) ? users : [users],
      name,
      host,

      username,
      password,
      allowedOrganizations: Array.isArray(allowedOrganizations)
        ? allowedOrganizations
        : [allowedOrganizations],
    });

    await credential.save();
    res.status(201).json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4001,
      "Failed to create ClickHouse credential",
      "createClickHouseCredential",
      error
    );
  }
};
// Get ClickHouse credential by ID
exports.getClickHouseCredentialById = async (req, res) => {
  const { id } = req.params;
  try {
    const credential = await ClickHouseCredential.findById(id)
      .populate({
        path: "users",
        select: "id name email",
      })
      .populate({
        path: "allowedOrganizations",
        select: "id name slug",
      });

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "getClickHouseCredentialById"
      );
    }

    res.json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4003,
      "Failed to fetch ClickHouse credential",
      "getClickHouseCredentialById",
      error
    );
  }
};

// Update ClickHouse credential
exports.updateClickHouseCredential = async (req, res) => {
  const { id } = req.params;
  const { users, name, host, port, username, password, allowedOrganizations } =
    req.body;

  try {
    const credential = await ClickHouseCredential.findById(id);

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "updateClickHouseCredential"
      );
    }

    credential.users = users || credential.users;
    credential.name = name || credential.name;
    credential.host = host || credential.host;
    credential.port = port || credential.port;
    credential.username = username || credential.username;
    credential.password = password || credential.password;
    credential.allowedOrganizations =
      allowedOrganizations || credential.allowedOrganizations;

    await credential.save();
    res.json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4004,
      "Failed to update ClickHouse credential",
      "updateClickHouseCredential",
      error
    );
  }
};

// Delete ClickHouse credential
exports.deleteClickHouseCredential = async (req, res) => {
  const { id } = req.params;

  try {
    const credential = await ClickHouseCredential.findByIdAndDelete(id);

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "deleteClickHouseCredential"
      );
    }

    res.json({ message: "ClickHouse credential deleted successfully" });
  } catch (error) {
    errorResponse(
      res,
      500,
      4005,
      "Failed to delete ClickHouse credential",
      "deleteClickHouseCredential",
      error
    );
  }
};

// Assign ClickHouse credential to an organization
exports.assignCredentialToOrganization = async (req, res) => {
  const { credentialId, organizationId } = req.body;

  try {
    const credential = await ClickHouseCredential.findById(credentialId);

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "assignCredentialToOrganization"
      );
    }

    if (!credential.allowedOrganizations.includes(organizationId)) {
      credential.allowedOrganizations.push(organizationId);
    }

    await credential.save();
    res.json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4006,
      "Failed to assign ClickHouse credential to organization",
      "assignCredentialToOrganization",
      error
    );
  }
};

// Revoke ClickHouse credential from an organization
exports.revokeCredentialFromOrganization = async (req, res) => {
  const { credentialId, organizationId } = req.body;

  try {
    const credential = await ClickHouseCredential.findById(credentialId);

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "revokeCredentialFromOrganization"
      );
    }

    credential.allowedOrganizations = credential.allowedOrganizations.filter(
      (id) => id.toString() !== organizationId
    );

    await credential.save();
    res.json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4007,
      "Failed to revoke ClickHouse credential from organization",
      "revokeCredentialFromOrganization",
      error
    );
  }
};

// Assign user to ClickHouse credential
exports.assignUserToCredential = async (req, res) => {
  const { credentialId, userId } = req.body;

  try {
    const credential = await ClickHouseCredential.findById(credentialId);

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "assignUserToCredential"
      );
    }

    if (!credential.users.includes(userId)) {
      credential.users.push(userId);
    }

    await credential.save();
    res.json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4008,
      "Failed to assign user to ClickHouse credential",
      "assignUserToCredential",
      error
    );
  }
};

// Revoke user from ClickHouse credential
exports.revokeUserFromCredential = async (req, res) => {
  const { credentialId, userId } = req.body;

  try {
    const credential = await ClickHouseCredential.findById(credentialId);

    if (!credential) {
      return errorResponse(
        res,
        404,
        4002,
        "ClickHouse credential not found",
        "revokeUserFromCredential"
      );
    }

    credential.users = credential.users.filter(
      (id) => id.toString() !== userId
    );

    await credential.save();
    res.json(credential);
  } catch (error) {
    errorResponse(
      res,
      500,
      4009,
      "Failed to revoke user from ClickHouse credential",
      "revokeUserFromCredential",
      error
    );
  }
};
