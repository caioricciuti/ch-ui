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
app.use("/api/v1/auth", require("./routes/auth.route")); // Added auth routes
app.use("/api/v1/users", require("./routes/user.route"));
app.use("/api/v1/organizations", require("./routes/organization.route")); // Added organization routes
app.use("/api/v1/credentials", require("./routes/clickHouseCredential.route")); // Added clickHouseCredential routes
app.use("/api/v1/query", require("./routes/query.route")); // Added query routes
// app.use("/api/v1/editors", require("./routes/editors.route")); // Added editors routes
// app.use("/api/v1/metrics", require("./routes/metrics.route")); // Added metrics routes

app.listen(process.env.PORT || 5124, () => {
  console.log(`Server is running on port ${process.env.PORT}`);
});
