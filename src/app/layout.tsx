import "./styles.css";
import "@fontsource/noto-sans-sc/chinese-simplified-400.css";
import "@react-pdf-viewer/core/lib/styles/index.css";
import "@react-pdf-viewer/highlight/lib/styles/index.css";
import "@react-pdf-viewer/page-navigation/lib/styles/index.css";
import "@react-pdf-viewer/search/lib/styles/index.css";
import "@react-pdf-viewer/zoom/lib/styles/index.css";
import { GlobalProgress } from "@/components/global-progress";
import {
  PreferencesControls,
  PreferencesProvider,
} from "@/features/preferences";
import type { Metadata } from "next";
export const metadata: Metadata = {
  title: "Review Hub",
  description: "本地优先的论文 PDF 审阅与历史追踪",
};
export default function Layout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body>
        <PreferencesProvider>
          <GlobalProgress />
          <PreferencesControls />
          {children}
        </PreferencesProvider>
      </body>
    </html>
  );
}
