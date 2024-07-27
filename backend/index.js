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
    origin: ["http://localhost:5173"], // process.env.CLIENT_URL,
    credentials: true,
  })
);

// routes
app.use("/api/v1/auth", require("./routes/auth.route")); // Added auth routes
app.use("/api/v1/users", require("./routes/user.route"));
// Middleware to introduce a delay of 3 seconds

app.use("/api/v1/organizations", require("./routes/organization.route")); // Added organization routes
app.use(
  "/api/v1/clickhouse-credentials",
  require("./routes/clickHouseCredential.route")
);
app.use("/api/v1/query", require("./routes/query.route"));
// app.use("/api/v1/editors", require("./routes/editors.route")); // Added editors routes
app.use("/api/v1/metrics", require("./routes/metric.route")); // Added metrics routes
app.use("/api/v1/ch-queries/", require("./routes/chUiQueries.route")); // Added queries routes

app.listen(process.env.PORT || 5124, () => {
  console.log(`Server is running on port ${process.env.PORT}`);
});
