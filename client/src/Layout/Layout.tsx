import React from "react";
import { Outlet } from "react-router-dom";
import Navbar from "@/components/Navbar";
import useAuthStore from "@/stores/user.store";
import { CommandMenu } from "@/components/CommandMenu";

const Layout: React.FC = () => {
  const { user } = useAuthStore();

  return (
    <div className="h-screen flex flex-col overflow-hidden">
      {user && <Navbar />}
      <main className="flex-1 flex overflow-hidden">
        <Outlet />
      </main>
      <CommandMenu />
    </div>
  );
};

export default Layout;
