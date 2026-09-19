import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "产品图工作台",
  description: "产品图独立应用(HUI-1739 I0 底座)",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <header className="topbar">
          <span className="brand">产品图工作台</span>
          <span className="sub">product-image-engine · I0 底座</span>
        </header>
        <main className="main">{children}</main>
      </body>
    </html>
  );
}
