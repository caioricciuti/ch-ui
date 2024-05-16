import { useEffect } from "react";
import { BrowserRouter as Router, Routes, Route, Link } from "react-router-dom";
import Sidebar from "./components/Sidebar";
import { Toaster } from "@/components/ui/sonner";
import { ClickHouseProvider } from "./providers/ClickHouseContext";
import HomePage from "./pages/HomePage";
import InstanceMetricsPage from "./pages/InstanceMetricsPage";
import SettingsPage from "./pages/SettingsPage";

export default function App() {
  useEffect(() => {
    window.onbeforeunload = (e) => {
      e.preventDefault();
      e.returnValue = null;
      return null;
    };
  }, []);

  return (
    <ClickHouseProvider>
      <div className="pl-[56px] h-screen">
        <Toaster
          className="overflow-visible"
          duration={2500}
          position="bottom-right"
          visibleToasts={9}
          expand={true}
        />

        <Router>
          <Sidebar />

          <Routes>
            <Route exact path="/" element={<HomePage />} />
            <Route
              path="/instance-metrics"
              element={<InstanceMetricsPage />}
            />
            <Route path="/settings" element={<SettingsPage />} />
            <Route
              path="*"
              element={
                <div className="flex justify-center items-center h-full">
                  <div className="flex flex-col justify-center items-center border rounded-md p-6 mt-20">
                    <h1 className="text-4xl font-bold">404 Not Found</h1>
                    <p className="text-lg mt-4">
                      The page you are looking for does not exist.
                    </p>
                    <div className="mt-6">
                      <Link className="text-primary hover:underline" to="/">
                        Go to Homepage
                      </Link>
                    </div>
                  </div>
                </div>
              }
            />
          </Routes>

        </Router>
      </div>
    </ClickHouseProvider>
  );
}
