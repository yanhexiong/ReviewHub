/** @type {import("next").NextConfig} */
const nextConfig = {
  // The release runtime is a static site served by the Go API, so no Next.js
  // server, Node.js binary, or node_modules directory is shipped in AppImage.
  output: "export",
  trailingSlash: true,
  webpack(config) {
    // PDF.js declares native canvas as an optional Node dependency. The
    // browser uses the DOM Canvas API and static export has no Node renderer.
    config.resolve.alias = {
      ...config.resolve.alias,
      canvas: false,
    };
    return config;
  },
};

export default nextConfig;
