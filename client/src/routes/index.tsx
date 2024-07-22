// src/routes/index.tsx

import {
  createBrowserRouter,
  RouterProvider,
  Navigate,
} from "react-router-dom";
import LoginPage from "@/Pages/Login";
import Main from "@/Pages/Main";
import Layout from "@/Layout/Layout";
import AuthGuard from "@/components/AuthGuard";
import RegisterPage from "@/Pages/Register";
import DashboardPage from "@/Pages/Dashboard";
import OrganizationsPage from "@/Pages/Organizations";
import WorkspacePage from "@/Pages/Workspace";
import AdminPage from "@/Pages/Admin";
import MetricsPage from "@/Pages/Metrics";
import ProfilePage from "@/Pages/Profile";

const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      {
        path: "login",
        element: <LoginPage />,
      },
      {
        path: "register",
        element: <RegisterPage />,
      },
      {
        element: <AuthGuard />,
        children: [
          {
            path: "/",
            element: <Main />,
          },
          {
            path: "dashboard",
            element: <DashboardPage />,
          },
          {
            path: "organizations",
            element: <OrganizationsPage />,
          },
          {
            path: "workspace",
            element: <WorkspacePage />,
          },
          {
            path: "admin",
            element: <AdminPage />,
          },
          {
            path: "metrics",
            element: <MetricsPage />,
          },
          {
            path: "profile",
            element: <ProfilePage />,
          },
          {
            path: "*",
            element: <Navigate to="/dashboard" />,
          },
        ],
      },
      {
        path: "*",
        element: <Navigate to="/login" replace />,
      },
    ],
  },
]);

const Routes = () => {
  return <RouterProvider router={router} />;
};

export default Routes;
