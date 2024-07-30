import React from "react";
import { Outlet } from "react-router-dom";
import Navbar from "@/components/Navbar";
import useAuthStore from "@/stores/user.store";
import { CommandMenu } from "@/components/CommandMenu";

const Layout: React.FC = () => {
  const { user } = useAuthStore();

  return (
    <div className="h-screen overflow-hidden">
      {user && <Navbar />}
      <main>
        <Outlet />
      </main>
      <CommandMenu />
    </div>
  );
};

export default Layout;
