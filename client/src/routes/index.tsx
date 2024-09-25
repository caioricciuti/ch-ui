// src/routes/index.tsx
import React from "react";
import { createBrowserRouter, RouterProvider } from "react-router-dom";
import generateRoutes from "./generateRoutes";

const Layout = React.lazy(() => import("@/Layout/Layout"));

const routes = generateRoutes();

const router = createBrowserRouter([
  {
    path: "/",
    element: React.createElement(
      React.Suspense,
      { fallback: React.createElement("div", null) },
      React.createElement(Layout)
    ),
    children: routes,
  },
]);

const Routes: React.FC = () => {
  return React.createElement(RouterProvider, { router });
};

export default Routes;
