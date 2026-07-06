import { defineConfig } from "vite";
import preact from "@preact/preset-vite";

// Builds to ../dist, which is embedded into the daemon binary via go:embed
// (see internal/server/webui.go). The build output is committed to git so
// `go build`/`go run ./cmd/aegis` work with no Node.js dependency; run
// `npm run build` here after editing src/ and commit the regenerated dist/.
export default defineConfig({
  plugins: [preact()],
  base: "/ui/",
  build: {
    outDir: "../dist",
    emptyOutDir: true,
    sourcemap: false,
  },
});
