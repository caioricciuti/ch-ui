const express = require("express");
const cookieParser = require("cookie-parser");
const cors = require("cors");
require("dotenv").config();
const { Server } = require("socket.io");

const app = express();

// database connection
const connectDB = require("./lib/db");
connectDB();

app.use(express.json());
app.use(cookieParser());
app.use(
  cors({
    origin: "http://localhost:5173",
    credentials: true,
  })
);

// routes
app.use("/api/v1/auth", require("./routes/auth.route"));
app.use("/api/v1/users", require("./routes/user.route"));
app.use("/api/v1/organizations", require("./routes/organization.route"));
app.use(
  "/api/v1/clickhouse-credentials",
  require("./routes/clickHouseCredential.route")
);
app.use("/api/v1/query", require("./routes/query.route"));
app.use("/api/v1/metrics", require("./routes/metric.route"));
app.use("/api/v1/ch-queries/", require("./routes/chUiQueries.route"));
app.use("/api/v1/notifications", require("./routes/notifications.route"));

app.listen(process.env.PORT, () => {
  console.log(`Server running on port ${process.env.PORT}`);
});
