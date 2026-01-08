import { BrowserRouter as Router, Routes, Route } from "react-router-dom";
import Sidebar from "@/components/common/Sidebar";
import HomePage from "@/pages/Home";
import MetricsPage from "@/pages/Metrics";
import SettingsPage from "@/pages/Settings";
import LoginPage from "@/pages/Login";
import { ThemeProvider } from "@/components/common/theme-provider";
import AppInitializer from "@/components/common/AppInit";
import NotFound from "./pages/NotFound";
import { PrivateRoute } from "@/components/common/privateRoute"; // Import PrivateRoute
import Admin from "@/pages/Admin";
import LogsPage from "@/pages/Logs";
import { AdminRoute } from "@/features/admin/routes/adminRoute";

export default function App() {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <Router basename={import.meta.env.BASE_URL}>
        <AppInitializer>
          <Routes>
            {/* Public routes (no sidebar) */}
            <Route path="/login" element={<LoginPage />} />

            {/* Protected routes with sidebar layout */}
            <Route
              path="/*"
              element={
                <PrivateRoute>
                  <div className="flex h-screen">
                    <Sidebar />
                    <Routes>
                      <Route path="/" element={<HomePage />} />
                      <Route path="/metrics" element={<MetricsPage />} />
                      <Route path="/logs" element={<LogsPage />} />
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
                    </Routes>
                  </div>
                </PrivateRoute>
              }
            />
          </Routes>
        </AppInitializer>
      </Router>
    </ThemeProvider>
  );
}
