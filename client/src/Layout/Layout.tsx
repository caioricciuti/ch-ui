import React from "react";
import { Outlet } from "react-router-dom";
import Navbar from "@/components/Navbar";
import useAuthStore from "@/stores/user.store";

const Layout: React.FC = () => {
  const { user } = useAuthStore();

  return (
    <main>
      {user && <Navbar />}
      <section className="max-w-screen-xl mx-auto">
        <Outlet />
      </section>
    </main>
  );
};

export default Layout;
