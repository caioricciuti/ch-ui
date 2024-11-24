import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";
import { Toaster } from "@/components/ui/sonner";

// Polyfill for crypto.randomUUID if not available
if (typeof crypto.randomUUID !== "function") {
  crypto.randomUUID = function () {
    return "10000000-1000-4000-8000-100000000000".replace(/[018]/g, (c) =>
      (
        parseInt(c) ^
        (crypto.getRandomValues(new Uint8Array(1))[0] &
          (15 >> (parseInt(c) / 4)))
      ).toString(16)
    ) as `${string}-${string}-${string}-${string}-${string}`;
  };
}

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <Toaster
      richColors
      toastOptions={{ duration: 2000, closeButton: true }}
      expand={true}
    />
    <App />
  </React.StrictMode>
);
