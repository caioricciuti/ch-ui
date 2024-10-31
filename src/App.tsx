import { BrowserRouter as Router, Routes, Route } from "react-router-dom";
import Sidebar from "./components/Sidebar";
import HomePage from "@/pages/Home";
import MetricsPage from "@/pages/Metrics";
import SettingsPage from "@/pages/Settings";
import { ThemeProvider } from "@/components/theme-provider";
import AppInitializer from "@/components/AppInit";
import NotFound from "./pages/NotFound";
import { PrivateRoute } from "@/components/privateRoute"; // Import PrivateRoute
import Admin from "@/pages/admin/Admin";
import { AdminRoute } from "@/components/admin/adminRoute";

export default function App() {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <Router basename="/ui">
        <AppInitializer>
          <div className="flex h-screen overflow-hidden">
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
