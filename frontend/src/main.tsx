import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Fonts are bundled so the app looks the same offline.
import "@fontsource-variable/atkinson-hyperlegible-next";
import "@fontsource-variable/geist-mono";
import "@/lib/monaco";
import "@/styles/globals.css";
import { AppRoot } from "@/app";
import { startZoom } from "@/lib/zoom";

startZoom();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppRoot />
  </StrictMode>,
);
