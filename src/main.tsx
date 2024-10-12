import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";
import { Toaster } from "@/components/ui/sonner";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <Toaster richColors toastOptions={{ duration: 2000, closeButton: true }} />
    <App />
  </React.StrictMode>
);
