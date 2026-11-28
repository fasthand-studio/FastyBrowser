import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ command }) => ({
  base: command === "build" ? "/products/fastybrowser/" : "/",
  plugins: [react()],
}));
