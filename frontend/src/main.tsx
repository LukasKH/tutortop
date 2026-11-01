import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Fonts are bundled so the app looks the same offline.
import "@fontsource-variable/atkinson-hyperlegible-next";
import "@fontsource-variable/geist-mono";
import "./monaco";
import "./styles.css";
import { AppRoot } from "./App";
import { startZoom } from "./zoom";

startZoom();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppRoot />
  </StrictMode>,
);
