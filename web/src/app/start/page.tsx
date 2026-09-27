import { Suspense } from "react";
import StartClient from "./ui";

export default function StartPage() {
  return (
    <Suspense fallback={<p>正在打开开始做产品图…</p>}>
      <StartClient />
    </Suspense>
  );
}
