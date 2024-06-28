// src/routes/index.tsx

import {
  createBrowserRouter,
  RouterProvider,
  Navigate,
} from "react-router-dom";
import Login from "@/pages/Login";
import Main from "@/pages/Main";
import Layout from "@/components/Layout/Layout";
import AuthGuard from "@/components/AuthGuard";

const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      {
        path: "login",
        element: <Login />,
      },
      {
        element: <AuthGuard />,
        children: [
          {
            path: "/",
            element: <Main />,
          },
          // Add other protected routes here
        ],
      },
      {
        path: "*",
        element: <Navigate to="/" replace />,
      },
    ],
  },
]);

const Routes = () => {
  return <RouterProvider router={router} />;
};

export default Routes;
