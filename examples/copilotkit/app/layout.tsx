import type { ReactNode } from "react";

export const metadata = {
  title: "Pando × CopilotKit",
  description: "Generative-UI frontend for a Pando agent over AG-UI",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
