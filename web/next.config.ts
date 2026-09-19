import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // 底座保持最小:不做图片优化等需要外部依赖的配置。
  reactStrictMode: true,
};

export default nextConfig;
