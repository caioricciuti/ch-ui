import React from "react";
import { Outlet } from "react-router-dom";
import Sidebar from "@/components/Sidebar"; // Update this import path as needed
import useAuthStore from "@/stores/user.store";
import { CommandMenu } from "@/components/CommandMenu";

const Layout: React.FC = () => {
  const { user } = useAuthStore();

  return (
    <div className="flex h-screen overflow-hidden">
      {user && <Sidebar />}
      <div className="flex-1 flex flex-col overflow-hidden">
        <main className="flex-1 overflow-y-auto">
          <Outlet />
        </main>
        <CommandMenu />
      </div>
    </div>
  );
};

export default Layout;
