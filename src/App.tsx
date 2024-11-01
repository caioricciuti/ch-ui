import { BrowserRouter as Router, Routes, Route } from "react-router-dom";
import Sidebar from "@/components/common/Sidebar";
import HomePage from "@/pages/Home";
import MetricsPage from "@/pages/Metrics";
import SettingsPage from "@/pages/Settings";
import { ThemeProvider } from "@/components/common/theme-provider";
import AppInitializer from "@/components/common/AppInit";
import NotFound from "./pages/NotFound";
import { PrivateRoute } from "@/components/common/privateRoute"; // Import PrivateRoute
import Admin from "@/pages/Admin";
import { AdminRoute } from "@/features/admin/routes/adminRoute";

export default function App() {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <Router>
        <AppInitializer>
          <div className="flex h-screen">
            <Sidebar />
            <Routes>
              <Route
                path="/"
                element={
                  <PrivateRoute>
                    <HomePage />
                  </PrivateRoute>
                }
              />
              <Route
                path="/metrics"
                element={
                  <PrivateRoute>
                    <MetricsPage />
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
            </Routes>
          </div>
        </AppInitializer>
      </Router>
    </ThemeProvider>
  );
}
