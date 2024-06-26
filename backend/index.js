const express = require("express");
const cookieParser = require("cookie-parser");
//cors
const cors = require("cors");
// dotenv
require("dotenv").config();

const app = express();

// database connection
const connectDB = require("./lib/db");
connectDB();

app.use(express.json());
app.use(cookieParser());
//cors
app.use(
  cors({
    origin: process.env.CLIENT_URL,
    credentials: true,
  })
);

// routes
app.use("/u", require("./routes/user.route"));
app.use("/o", require("./routes/organization.route")); // Added organization routes
app.use("/c", require("./routes/clickHouseCredential.routes")); // Added clickHouseCredential routes
app.use("/q", require("./routes/query.route")); // Added query routes

app.listen(process.env.PORT, () => {
  console.log(`Server is running on port ${process.env.PORT}`);
});
