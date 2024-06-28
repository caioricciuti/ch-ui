// src/components/Layout/Layout.tsx

import React from "react";
import { Outlet } from "react-router-dom";
import Navbar from "@/components/Navbar";
import useAuthStore from "@/stores/user.store";

const Layout: React.FC = () => {
  const { isAuthenticated } = useAuthStore();

  return (
    <div className="min-h-screen bg-gray-100">
      {isAuthenticated && <Navbar />}
      <main className="container mx-auto p-4">
        <Outlet />
      </main>
    </div>
  );
};

export default Layout;
