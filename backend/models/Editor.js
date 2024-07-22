// Monaco Editor Model For Editors Collection

const mongoose = require("mongoose");

// Monaco Editor Model (Editors Collection)
const editorsSchema = new mongoose.Schema(
  {
    name: {
      type: String,
      required: [true, "Editor name is required"],
      trim: true,
    },
    content: {
      type: String,
      required: [true, "Editor content is required"],
    },
    language: {
      type: String,
      required: [true, "Editor language is required"],
    },
    user: {
      type: mongoose.Schema.Types.ObjectId,
      ref: "User",
      required: true,
    },
    organization: {
      type: mongoose.Schema.Types.ObjectId,
      ref: "Organization",
      required: true,
    },
  },
  { timestamps: true }
);

const Editor = mongoose.model("Editor", editorsSchema);

module.exports = Editor;