// Monaco Editor Model For Editors Collection

const mongoose = require("mongoose");

const editorsSchema = new mongoose.Schema({});

module.exports = mongoose.model("Editors", editorsSchema);

