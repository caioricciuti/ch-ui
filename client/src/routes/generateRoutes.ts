// src/routes/generateRoutes.ts
import { RouteObject } from "react-router-dom";
import React from "react";

function generateRoutes(): RouteObject[] {
  const routes: RouteObject[] = [];
  const protectedRoutes: RouteObject[] = [];

  // Vite's import.meta.glob for dynamic imports
  const pages = import.meta.glob("../pages/**/*.tsx");

  Object.keys(pages).forEach((key) => {
    const path = key
      .replace("../pages/", "")
      .replace(/\.tsx$/, "")
      .replace(/index$/, "")
      .replace(/\[([^\]]+)\]/g, ":$1")
      .toLowerCase();

    const Component = React.lazy(
      pages[key] as () => Promise<{ default: React.ComponentType }>
    );

    const route: RouteObject = {
      path: path === "" ? "/" : `/${path}`,
      element: React.createElement(React.Suspense, {
        fallback: React.createElement("div", null),
        children: React.createElement(Component),
      }),
    };

    if (path.startsWith("account/")) {
      routes.push(route);
    } else {
      protectedRoutes.push(route);
    }
  });

  const AuthGuard = React.lazy(() => import("../components/AuthGuard"));
  routes.push({
    element: React.createElement(React.Suspense, {
      fallback: React.createElement("div", null),
      children: React.createElement(AuthGuard),
    }),
    children: protectedRoutes,
  });

  const NotFound = React.lazy(() => import("../pages/NotFound"));
  routes.push({
    path: "*",
    element: React.createElement(React.Suspense, {
      fallback: React.createElement("div", null, "Loading..."),
      children: React.createElement(NotFound),
    }),
  });

  return routes;
}

export default generateRoutes;
