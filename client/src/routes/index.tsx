// src/routes/index.tsx

import {
  createBrowserRouter,
  RouterProvider,
  Navigate,
} from "react-router-dom";
import LoginPage from "@/pages/Login";
import Main from "@/pages/Main";
import Layout from "@/Layout/Layout";
import AuthGuard from "@/components/AuthGuard";
import RegisterPage from "@/pages/Register";
import OrganizationsPage from "@/pages/Organizations";
import WorkspacePage from "@/pages/Workspace";
import AdminPage from "@/pages/Admin";
import MetricsPage from "@/pages/Metrics";
import ActivateAccount from "@/pages/ActivateAccount";
import SettingsPage from "@/pages/Settings";
import CredentialsPage from "@/pages/Credentials";

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
        path: "activate-account/:activationToken",
        element: <ActivateAccount />,
      },
      {
        element: <AuthGuard />,
        children: [
          {
            path: "/",
            element: <Main />,
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
            path: "Settings",
            element: <SettingsPage />,
          },
          {
            path: "credentials",
            element: <CredentialsPage />,
          },
          {
            path: "*",
            element: <Navigate to="/" />,
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
