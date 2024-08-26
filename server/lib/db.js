// connect mongodb with mongoose

const mongoose = require("mongoose");

const connectDB = async () => {
  try {
    await mongoose.connect(process.env.MONGO_URI);
    console.log("MongoDB connection SUCCESS");
  } catch (error) {
    console.error(`MongoDB connection FAIL: ${error}`);
    process.exit(1);
  }
};

module.exports = connectDB;
