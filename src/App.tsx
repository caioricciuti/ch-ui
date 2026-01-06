import { BrowserRouter as Router, Routes, Route, Outlet } from "react-router-dom";
import Sidebar from "@/components/common/Sidebar";
import HomePage from "@/pages/Home";
import MetricsPage from "@/pages/Metrics";
import SettingsPage from "@/pages/Settings";
import { RestrictedRoute } from "@/components/common/RestrictedRoute";
import { ThemeProvider } from "@/components/common/theme-provider";
import AppInitializer from "@/components/common/AppInit";
import NotFound from "./pages/NotFound";
import { PrivateRoute } from "@/components/common/privateRoute";
import Admin from "@/pages/Admin";
import LogsPage from "@/pages/Logs";
import Login from "@/pages/Login";
import ExplorerPage from "@/pages/Explorer";
import { AdminRoute } from "@/features/admin/routes/adminRoute";

// Layout for the main application (authenticated routes)
const MainLayout = () => {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-[#0a0a0a] bg-[radial-gradient(ellipse_80%_80%_at_50%_-20%,rgba(120,119,198,0.3),rgba(255,255,255,0))] relative">
      {/* Background Decor */}
      <div className="absolute inset-0 z-0 pointer-events-none">
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-purple-500/10 rounded-full blur-3xl opacity-50" />
        <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-blue-500/10 rounded-full blur-3xl opacity-50" />
      </div>

      <Sidebar />
      <main className="flex-1 h-full overflow-auto z-10 relative">
        <Outlet />
      </main>
    </div>
  );
};

export default function App() {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <Router basename={import.meta.env.BASE_URL}>
        <AppInitializer>
          <Routes>
            {/* Public/Standalone Routes */}
            <Route path="/login" element={<Login />} />

            {/* Authenticated Application Routes */}
            <Route element={<MainLayout />}>
              <Route
                path="/"
                element={
                  <RestrictedRoute>
                    <HomePage />
                  </RestrictedRoute>
                }
              />
              <Route
                path="/metrics"
                element={
                  <RestrictedRoute>
                    <MetricsPage />
                  </RestrictedRoute>
                }
              />
              <Route
                path="/logs"
                element={
                  <PrivateRoute>
                    <LogsPage />
                  </PrivateRoute>
                }
              />
              <Route
                path="/explorer"
                element={
                  <PrivateRoute>
                    <ExplorerPage />
                  </PrivateRoute>
                }
              />
              <Route
                path="/admin"
                element={
                  <AdminRoute>
                    <Admin />
                  </AdminRoute>
                }
              />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
        </AppInitializer>
      </Router>
    </ThemeProvider>
  );
}
