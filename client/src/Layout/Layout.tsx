import React from "react";
import { Outlet } from "react-router-dom";
import Navbar from "@/components/Navbar";
import useAuthStore from "@/stores/user.store";
import { CommandMenu } from "@/components/CommandMenu";

const Layout: React.FC = () => {
  const { user } = useAuthStore();

  return (
    <main>
      {user && <Navbar />}
      <section className="mx-auto">
        <Outlet />
        <CommandMenu />
      </section>
    </main>
  );
};

export default Layout;
